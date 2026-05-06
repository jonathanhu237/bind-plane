import asyncio

from bind_plane.db.models import (
    AuditLog,
    CommandProfile,
    Credential,
    ImportBatch,
    ImportBatchStatus,
    Network,
    ReleaseJob,
    ReleaseJobKind,
    ReleaseJobPhase,
    ReleaseJobStatus,
    RoleName,
    Switch,
    User,
    UserRole,
)
from bind_plane.security.passwords import hash_password
from fastapi.testclient import TestClient
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker


async def _seed_list_environment(session_factory: async_sessionmaker[AsyncSession]) -> None:
    async with session_factory() as session:
        admin = User(
            username="admin",
            password_hash=hash_password("password123"),
            display_name="Admin",
            must_change_password=False,
        )
        admin.roles = [UserRole(role=RoleName.ADMIN)]
        operator = User(
            username="operator",
            password_hash=hash_password("password123"),
            display_name="Operator",
            must_change_password=False,
        )
        operator.roles = [UserRole(role=RoleName.OPERATOR)]
        other_operator = User(
            username="other",
            password_hash=hash_password("password123"),
            display_name="Other Operator",
            must_change_password=False,
        )
        other_operator.roles = [UserRole(role=RoleName.OPERATOR)]
        active_credential = Credential(
            name="lab",
            username="netops",
            encrypted_password="encrypted",
            is_active=True,
        )
        inactive_credential = Credential(
            name="legacy",
            username="archive",
            encrypted_password="encrypted",
            is_active=False,
        )
        h3c_profile = CommandProfile(name="h3c", command_templates={}, parser_rules={})
        arista_profile = CommandProfile(name="arista", command_templates={}, parser_rules={})
        session.add_all(
            [
                admin,
                operator,
                other_operator,
                active_credential,
                inactive_credential,
                h3c_profile,
                arista_profile,
            ]
        )
        await session.flush()
        edge_switch = Switch(
            name="edge-sw-01",
            management_ip="10.0.0.10",
            credential_id=active_credential.id,
            command_profile_id=h3c_profile.id,
            is_enabled=True,
        )
        disabled_switch = Switch(
            name="old-sw-01",
            management_ip="10.0.0.20",
            credential_id=inactive_credential.id,
            command_profile_id=arista_profile.id,
            is_enabled=False,
        )
        session.add_all([edge_switch, disabled_switch])
        await session.flush()
        session.add_all(
            [
                Network(
                    switch_id=edge_switch.id,
                    cidr="10.44.132.0/24",
                    prefix_length=24,
                    is_validated=True,
                ),
                Network(
                    switch_id=disabled_switch.id,
                    cidr="10.44.133.0/24",
                    prefix_length=24,
                    is_validated=False,
                ),
            ]
        )
        session.add_all(
            [
                ReleaseJob(
                    target_ip="10.44.132.10",
                    kind=ReleaseJobKind.RELEASE,
                    reason="temporary_test",
                    force=False,
                    status=ReleaseJobStatus.FAILED,
                    phase=ReleaseJobPhase.FINISHED,
                    operator_id=operator.id,
                    switch_id=edge_switch.id,
                    command_profile_id=h3c_profile.id,
                ),
                ReleaseJob(
                    target_ip="10.44.132.11",
                    kind=ReleaseJobKind.RELEASE,
                    reason="temporary_test",
                    force=True,
                    status=ReleaseJobStatus.SUCCEEDED,
                    phase=ReleaseJobPhase.FINISHED,
                    operator_id=operator.id,
                    switch_id=edge_switch.id,
                    command_profile_id=h3c_profile.id,
                ),
                ReleaseJob(
                    target_ip="10.44.132.12",
                    kind=ReleaseJobKind.RELEASE,
                    reason="temporary_test",
                    force=False,
                    status=ReleaseJobStatus.FAILED,
                    phase=ReleaseJobPhase.FINISHED,
                    operator_id=other_operator.id,
                    switch_id=edge_switch.id,
                    command_profile_id=h3c_profile.id,
                ),
                ImportBatch(
                    kind="switch_network",
                    source_filename="switches.json",
                    status=ImportBatchStatus.APPLIED,
                    summary={},
                    created_by_id=admin.id,
                ),
                ImportBatch(
                    kind="switch_network",
                    source_filename="bad.json",
                    status=ImportBatchStatus.FAILED,
                    summary={},
                    created_by_id=admin.id,
                ),
                AuditLog(
                    actor_id=admin.id,
                    action="release_job_completed",
                    target_type="release_job",
                    payload={},
                ),
                AuditLog(
                    actor_id=admin.id,
                    action="user_created",
                    target_type="user",
                    payload={},
                ),
            ]
        )
        await session.commit()


def _headers(client: TestClient, username: str) -> dict[str, str]:
    response = client.post(
        "/api/auth/login",
        json={"username": username, "password": "password123"},
    )
    assert response.status_code == 200
    return {"Authorization": f"Bearer {response.json()['access_token']}"}


def test_release_job_history_paginates_filters_and_scopes_to_operator(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_list_environment(session_factory))
    operator_headers = _headers(client, "operator")
    admin_headers = _headers(client, "admin")

    operator_response = client.get(
        "/api/releases/jobs?status=failed&page=1&page_size=1",
        headers=operator_headers,
    )
    admin_response = client.get(
        "/api/releases/jobs?status=failed&page=1&page_size=1&sort_by=target_ip&sort_order=asc",
        headers=admin_headers,
    )
    invalid_sort_response = client.get(
        "/api/releases/jobs?sort_by=operator_password",
        headers=admin_headers,
    )

    assert operator_response.status_code == 200
    operator_payload = operator_response.json()
    assert operator_payload["total"] == 1
    assert operator_payload["page_count"] == 1
    assert operator_payload["items"][0]["target_ip"] == "10.44.132.10"

    assert admin_response.status_code == 200
    admin_payload = admin_response.json()
    assert admin_payload["total"] == 2
    assert admin_payload["page_count"] == 2
    assert admin_payload["items"][0]["target_ip"] == "10.44.132.10"

    assert invalid_sort_response.status_code == 422


def test_admin_list_endpoints_paginate_filter_and_sort(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_list_environment(session_factory))
    admin_headers = _headers(client, "admin")

    users_response = client.get(
        "/api/admin/users?search=oper&page_size=1&sort_by=username&sort_order=asc",
        headers=admin_headers,
    )
    credentials_response = client.get(
        "/api/admin/credentials?is_active=false",
        headers=admin_headers,
    )
    profiles_response = client.get(
        "/api/admin/command-profiles?search=arista",
        headers=admin_headers,
    )
    switches_response = client.get(
        "/api/admin/switches?is_enabled=true&is_validated=true&search=edge",
        headers=admin_headers,
    )
    imports_response = client.get(
        "/api/admin/imports?status=applied",
        headers=admin_headers,
    )
    audit_response = client.get(
        "/api/audit?action=release_job_completed&sort_by=action&sort_order=asc",
        headers=admin_headers,
    )

    assert users_response.status_code == 200
    assert users_response.json()["total"] == 2
    assert len(users_response.json()["items"]) == 1

    assert credentials_response.status_code == 200
    assert credentials_response.json()["total"] == 1
    assert credentials_response.json()["items"][0]["name"] == "legacy"

    assert profiles_response.status_code == 200
    assert profiles_response.json()["total"] == 1
    assert profiles_response.json()["items"][0]["name"] == "arista"

    assert switches_response.status_code == 200
    assert switches_response.json()["total"] == 1
    assert switches_response.json()["items"][0]["name"] == "edge-sw-01"

    assert imports_response.status_code == 200
    assert imports_response.json()["total"] == 1
    assert imports_response.json()["items"][0]["status"] == "applied"

    assert audit_response.status_code == 200
    assert audit_response.json()["total"] == 1
    assert audit_response.json()["items"][0]["action"] == "release_job_completed"

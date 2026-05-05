from ipaddress import ip_address, ip_network
from typing import Any
from uuid import UUID

from fastapi import APIRouter, HTTPException, status
from sqlalchemy import select
from sqlalchemy.orm import selectinload

from bind_plane.api.deps import AdminUserDep, SessionDep
from bind_plane.db.models import (
    CommandProfile,
    Credential,
    ImportBatch,
    ImportBatchStatus,
    ImportIssue,
    Network,
    Switch,
)
from bind_plane.schemas.admin import (
    ImportBatchRead,
    SwitchNetworkImportRecord,
    SwitchNetworkImportRequest,
    SwitchRead,
)

router = APIRouter(prefix="/admin", tags=["admin-imports"])


async def _validate_import_record(
    session: SessionDep,
    row_number: int,
    record: SwitchNetworkImportRecord,
) -> list[ImportIssue]:
    issues: list[ImportIssue] = []
    try:
        ip_address(record.management_ip)
    except ValueError:
        issues.append(
            ImportIssue(
                row_number=row_number,
                severity="error",
                message="Management IP is invalid",
                payload={"management_ip": record.management_ip},
            )
        )

    try:
        parsed_network = ip_network(record.cidr, strict=False)
        if parsed_network.version != 4:
            issues.append(
                ImportIssue(
                    row_number=row_number,
                    severity="error",
                    message="Only IPv4 networks are supported",
                    payload={"cidr": record.cidr},
                )
            )
    except ValueError:
        issues.append(
            ImportIssue(
                row_number=row_number,
                severity="error",
                message="CIDR is invalid",
                payload={"cidr": record.cidr},
            )
        )

    credential = await session.get(Credential, record.credential_id)
    if credential is None:
        issues.append(
            ImportIssue(
                row_number=row_number,
                severity="error",
                message="Credential was not found",
                payload={"credential_id": str(record.credential_id)},
            )
        )

    profile = await session.get(CommandProfile, record.command_profile_id)
    if profile is None:
        issues.append(
            ImportIssue(
                row_number=row_number,
                severity="error",
                message="Command profile was not found",
                payload={"command_profile_id": str(record.command_profile_id)},
            )
        )

    return issues


async def _upsert_switch_and_network(
    session: SessionDep,
    record: SwitchNetworkImportRecord,
) -> None:
    parsed_network = ip_network(record.cidr, strict=False)
    switch = await session.scalar(
        select(Switch).where(Switch.management_ip == record.management_ip)
    )
    if switch is None:
        switch = Switch(
            name=record.switch_name,
            management_ip=record.management_ip,
            vendor=record.vendor,
            model=record.model,
            location=record.location,
            credential_id=record.credential_id,
            command_profile_id=record.command_profile_id,
            is_enabled=record.switch_enabled,
        )
        session.add(switch)
        await session.flush()
    else:
        switch.name = record.switch_name
        switch.vendor = record.vendor
        switch.model = record.model
        switch.location = record.location
        switch.credential_id = record.credential_id
        switch.command_profile_id = record.command_profile_id
        switch.is_enabled = record.switch_enabled

    network = await session.scalar(
        select(Network).where(Network.switch_id == switch.id, Network.cidr == str(parsed_network))
    )
    if network is None:
        network = Network(
            switch_id=switch.id,
            cidr=str(parsed_network),
            prefix_length=parsed_network.prefixlen,
            vlan=record.vlan,
            description=record.description,
            is_enabled=record.network_enabled,
            is_validated=record.network_validated,
        )
        session.add(network)
    else:
        network.prefix_length = parsed_network.prefixlen
        network.vlan = record.vlan
        network.description = record.description
        network.is_enabled = record.network_enabled
        network.is_validated = record.network_validated


def _summary_payload(
    *,
    payload: SwitchNetworkImportRequest,
    issues: list[ImportIssue],
    applied: bool,
) -> dict[str, Any]:
    errors = [issue for issue in issues if issue.severity == "error"]
    return {
        "records": len(payload.records),
        "issues": len(issues),
        "errors": len(errors),
        "applied": applied,
    }


@router.get("/switches")
async def list_switches(
    session: SessionDep,
    _: AdminUserDep,
) -> list[SwitchRead]:
    result = await session.execute(
        select(Switch).options(selectinload(Switch.networks)).order_by(Switch.name)
    )
    return [SwitchRead.model_validate(switch) for switch in result.scalars().all()]


@router.post("/imports/switch-networks")
async def import_switch_networks(
    payload: SwitchNetworkImportRequest,
    session: SessionDep,
    current_admin: AdminUserDep,
) -> ImportBatchRead:
    issues: list[ImportIssue] = []
    for row_number, record in enumerate(payload.records, start=1):
        issues.extend(await _validate_import_record(session, row_number, record))

    has_errors = any(issue.severity == "error" for issue in issues)
    batch = ImportBatch(
        kind="switch_network",
        source_filename=payload.source_filename,
        status=ImportBatchStatus.FAILED if has_errors else ImportBatchStatus.APPLIED,
        summary=_summary_payload(payload=payload, issues=issues, applied=not has_errors),
        created_by_id=current_admin.id,
    )
    batch.issues = issues
    session.add(batch)

    if not has_errors:
        for record in payload.records:
            await _upsert_switch_and_network(session, record)

    await session.commit()
    await session.refresh(batch, attribute_names=["issues"])
    return ImportBatchRead.model_validate(batch)


@router.get("/imports")
async def list_import_batches(
    session: SessionDep,
    _: AdminUserDep,
) -> list[ImportBatchRead]:
    result = await session.execute(
        select(ImportBatch)
        .options(selectinload(ImportBatch.issues))
        .order_by(ImportBatch.created_at.desc())
    )
    return [ImportBatchRead.model_validate(batch) for batch in result.scalars().all()]


@router.get("/imports/{batch_id}")
async def get_import_batch(
    batch_id: UUID,
    session: SessionDep,
    _: AdminUserDep,
) -> ImportBatchRead:
    result = await session.execute(
        select(ImportBatch)
        .options(selectinload(ImportBatch.issues))
        .where(ImportBatch.id == batch_id)
    )
    batch = result.scalar_one_or_none()
    if batch is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Import batch not found")
    return ImportBatchRead.model_validate(batch)

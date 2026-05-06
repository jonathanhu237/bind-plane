from typing import Literal
from uuid import UUID

from fastapi import APIRouter, HTTPException, Query, status
from sqlalchemy import or_, select

from bind_plane.api.deps import AdminUserDep, SessionDep
from bind_plane.api.pagination import apply_sort, paginate_query
from bind_plane.db.models import Credential
from bind_plane.schemas.admin import CredentialCreate, CredentialRead, CredentialUpdate
from bind_plane.schemas.pagination import PaginatedResponse
from bind_plane.security.credentials import encrypt_secret

router = APIRouter(prefix="/admin/credentials", tags=["admin-credentials"])


@router.get("")
async def list_credentials(
    session: SessionDep,
    _: AdminUserDep,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=25, ge=1, le=200),
    search: str | None = Query(default=None, max_length=128),
    is_active: bool | None = Query(default=None),
    sort_by: str = Query(default="name", max_length=64),
    sort_order: Literal["asc", "desc"] = Query(default="asc"),
) -> PaginatedResponse[CredentialRead]:
    query = select(Credential)
    if search:
        search_term = f"%{search.strip()}%"
        query = query.where(
            or_(
                Credential.name.ilike(search_term),
                Credential.username.ilike(search_term),
                Credential.description.ilike(search_term),
            )
        )
    if is_active is not None:
        query = query.where(Credential.is_active == is_active)
    query = apply_sort(
        query,
        sort_by=sort_by,
        sort_order=sort_order,
        allowed={
            "name": Credential.name,
            "username": Credential.username,
            "created_at": Credential.created_at,
            "updated_at": Credential.updated_at,
            "is_active": Credential.is_active,
        },
    )
    return await paginate_query(
        session,
        query,
        page=page,
        page_size=page_size,
        item_factory=CredentialRead.model_validate,
    )


@router.post("")
async def create_credential(
    payload: CredentialCreate,
    session: SessionDep,
    _: AdminUserDep,
) -> CredentialRead:
    existing = await session.scalar(select(Credential).where(Credential.name == payload.name))
    if existing is not None:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Credential name already exists",
        )

    credential = Credential(
        name=payload.name,
        username=payload.username,
        encrypted_password=encrypt_secret(payload.password),
        encrypted_secret=encrypt_secret(payload.secret) if payload.secret else None,
        description=payload.description,
        is_active=payload.is_active,
    )
    session.add(credential)
    await session.commit()
    await session.refresh(credential)
    return CredentialRead.model_validate(credential)


@router.patch("/{credential_id}")
async def update_credential(
    credential_id: UUID,
    payload: CredentialUpdate,
    session: SessionDep,
    _: AdminUserDep,
) -> CredentialRead:
    credential = await session.get(Credential, credential_id)
    if credential is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Credential not found")

    fields = payload.model_fields_set
    if "name" in fields and payload.name is not None:
        credential.name = payload.name
    if "username" in fields and payload.username is not None:
        credential.username = payload.username
    if "password" in fields and payload.password is not None:
        credential.encrypted_password = encrypt_secret(payload.password)
    if "secret" in fields:
        credential.encrypted_secret = encrypt_secret(payload.secret) if payload.secret else None
    if "description" in fields:
        credential.description = payload.description
    if "is_active" in fields and payload.is_active is not None:
        credential.is_active = payload.is_active

    await session.commit()
    await session.refresh(credential)
    return CredentialRead.model_validate(credential)


@router.delete("/{credential_id}")
async def deactivate_credential(
    credential_id: UUID,
    session: SessionDep,
    _: AdminUserDep,
) -> CredentialRead:
    credential = await session.get(Credential, credential_id)
    if credential is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Credential not found")
    credential.is_active = False
    await session.commit()
    await session.refresh(credential)
    return CredentialRead.model_validate(credential)

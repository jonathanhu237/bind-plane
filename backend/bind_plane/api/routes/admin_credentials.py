from uuid import UUID

from fastapi import APIRouter, HTTPException, status
from sqlalchemy import select

from bind_plane.api.deps import AdminUserDep, SessionDep
from bind_plane.db.models import Credential
from bind_plane.schemas.admin import CredentialCreate, CredentialRead, CredentialUpdate
from bind_plane.security.credentials import encrypt_secret

router = APIRouter(prefix="/admin/credentials", tags=["admin-credentials"])


@router.get("")
async def list_credentials(
    session: SessionDep,
    _: AdminUserDep,
) -> list[CredentialRead]:
    result = await session.execute(select(Credential).order_by(Credential.name))
    return [CredentialRead.model_validate(credential) for credential in result.scalars().all()]


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

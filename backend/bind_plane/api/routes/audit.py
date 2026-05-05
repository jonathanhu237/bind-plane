from fastapi import APIRouter, Query
from sqlalchemy import desc, select

from bind_plane.api.deps import AdminUserDep, SessionDep
from bind_plane.db.models import AuditLog
from bind_plane.schemas.audit import AuditLogRead

router = APIRouter(prefix="/audit", tags=["audit"])


@router.get("")
async def list_audit_logs(
    session: SessionDep,
    _: AdminUserDep,
    limit: int = Query(default=100, ge=1, le=500),
) -> list[AuditLogRead]:
    query = select(AuditLog).order_by(desc(AuditLog.created_at)).limit(limit)

    result = await session.execute(query)
    return [AuditLogRead.model_validate(log) for log in result.scalars().all()]

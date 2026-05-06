from typing import Literal

from fastapi import APIRouter, Query
from sqlalchemy import or_, select

from bind_plane.api.deps import AdminUserDep, SessionDep
from bind_plane.api.pagination import apply_sort, paginate_query
from bind_plane.db.models import AuditLog
from bind_plane.schemas.audit import AuditLogRead
from bind_plane.schemas.pagination import PaginatedResponse

router = APIRouter(prefix="/audit", tags=["audit"])


@router.get("")
async def list_audit_logs(
    session: SessionDep,
    _: AdminUserDep,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=25, ge=1, le=200),
    search: str | None = Query(default=None, max_length=128),
    action: str | None = Query(default=None, max_length=128),
    target_type: str | None = Query(default=None, max_length=128),
    sort_by: str = Query(default="created_at", max_length=64),
    sort_order: Literal["asc", "desc"] = Query(default="desc"),
) -> PaginatedResponse[AuditLogRead]:
    query = select(AuditLog)
    if search:
        search_term = f"%{search.strip()}%"
        query = query.where(
            or_(
                AuditLog.action.ilike(search_term),
                AuditLog.target_type.ilike(search_term),
            )
        )
    if action:
        query = query.where(AuditLog.action == action)
    if target_type:
        query = query.where(AuditLog.target_type == target_type)
    query = apply_sort(
        query,
        sort_by=sort_by,
        sort_order=sort_order,
        allowed={
            "created_at": AuditLog.created_at,
            "action": AuditLog.action,
            "target_type": AuditLog.target_type,
        },
    )

    return await paginate_query(
        session,
        query,
        page=page,
        page_size=page_size,
        item_factory=AuditLogRead.model_validate,
    )

from collections.abc import Callable, Mapping
from math import ceil
from typing import Literal

from fastapi import HTTPException
from sqlalchemy import Select, asc, desc, func, select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.sql.elements import ColumnElement

from bind_plane.schemas.pagination import PaginatedResponse

SortOrder = Literal["asc", "desc"]


def apply_sort[T](
    query: Select[tuple[T]],
    *,
    sort_by: str,
    sort_order: SortOrder,
    allowed: Mapping[str, ColumnElement[object]],
) -> Select[tuple[T]]:
    sort_column = allowed.get(sort_by)
    if sort_column is None:
        raise HTTPException(
            status_code=422,
            detail=f"Unsupported sort field: {sort_by}",
        )
    sort_expression = desc(sort_column) if sort_order == "desc" else asc(sort_column)
    return query.order_by(sort_expression)


async def paginate_query[T, R](
    session: AsyncSession,
    query: Select[tuple[T]],
    *,
    page: int,
    page_size: int,
    item_factory: Callable[[T], R],
) -> PaginatedResponse[R]:
    count_query = select(func.count()).select_from(
        query.order_by(None).limit(None).offset(None).subquery()
    )
    total = await session.scalar(count_query) or 0
    result = await session.execute(query.offset((page - 1) * page_size).limit(page_size))
    items = [item_factory(item) for item in result.scalars().unique().all()]
    return PaginatedResponse(
        items=items,
        total=total,
        page=page,
        page_size=page_size,
        page_count=ceil(total / page_size) if total else 0,
    )

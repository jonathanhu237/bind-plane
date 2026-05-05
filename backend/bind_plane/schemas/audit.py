from datetime import datetime
from typing import Any
from uuid import UUID

from pydantic import BaseModel, ConfigDict


class AuditLogRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: UUID
    actor_id: UUID
    action: str
    target_type: str
    target_id: UUID | None
    payload: dict[str, Any]
    created_at: datetime

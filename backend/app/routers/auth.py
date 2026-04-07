from fastapi import APIRouter, Cookie, Depends, HTTPException, Request, Response, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.database import get_db
from app.dependencies.auth import get_current_user, get_session_store
from app.models.user import User
from app.schemas.auth import LoginRequest, UserResponse
from app.services import auth as auth_service
from app.services.auth import AuthenticationError, InactiveUserError
from app.session import SessionStore

router = APIRouter(prefix="/api/auth", tags=["auth"])


def _set_session_cookie(
    response: Response, request: Request, session_id: str, expires_in: int
) -> None:
    response.set_cookie(
        key="session_id",
        value=session_id,
        max_age=expires_in,
        httponly=True,
        secure=request.url.scheme == "https",
        samesite="lax",
        path="/",
    )


def _clear_session_cookie(response: Response, request: Request) -> None:
    response.delete_cookie(
        key="session_id",
        httponly=True,
        secure=request.url.scheme == "https",
        samesite="lax",
        path="/",
    )


@router.post("/login", response_model=UserResponse)
async def login(
    body: LoginRequest,
    request: Request,
    response: Response,
    db: AsyncSession = Depends(get_db),
    session_store: SessionStore = Depends(get_session_store),
):
    try:
        result = await auth_service.login(
            body.username, body.password, db, session_store
        )
    except AuthenticationError:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid username or password",
        )
    except InactiveUserError:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="User account is disabled",
        )

    _set_session_cookie(
        response, request, result.session.session_id, result.session.expires_in
    )
    return result.user


@router.post("/logout", status_code=status.HTTP_204_NO_CONTENT)
async def logout(
    request: Request,
    response: Response,
    session_id: str | None = Cookie(default=None),
    session_store: SessionStore = Depends(get_session_store),
):
    if session_id is not None:
        await auth_service.logout(session_id, session_store)
    _clear_session_cookie(response, request)


@router.get("/me", response_model=UserResponse)
async def me(current_user: User = Depends(get_current_user)):
    return current_user

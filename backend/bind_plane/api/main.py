from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from bind_plane.api.routes import (
    admin_command_profiles,
    admin_credentials,
    admin_imports,
    admin_users,
    audit,
    auth,
    releases,
)
from bind_plane.core.config import get_settings


def create_app() -> FastAPI:
    settings = get_settings()
    app = FastAPI(title="bind-plane", debug=settings.debug)
    if settings.frontend_origin is not None:
        app.add_middleware(
            CORSMiddleware,
            allow_origins=[str(settings.frontend_origin).rstrip("/")],
            allow_credentials=True,
            allow_methods=["*"],
            allow_headers=["*"],
        )
    app.include_router(auth.router, prefix="/api")
    app.include_router(releases.router, prefix="/api")
    app.include_router(audit.router, prefix="/api")
    app.include_router(admin_users.router, prefix="/api")
    app.include_router(admin_credentials.router, prefix="/api")
    app.include_router(admin_command_profiles.router, prefix="/api")
    app.include_router(admin_imports.router, prefix="/api")

    @app.get("/health")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    return app


app = create_app()

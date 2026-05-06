from bind_plane.api.main import create_app
from bind_plane.core.config import Settings
from fastapi.testclient import TestClient


def test_fastapi_startup_runs_initial_admin_bootstrap() -> None:
    calls: list[Settings] = []

    async def bootstrap(settings: Settings) -> None:
        calls.append(settings)

    app = create_app(bootstrap_admin=bootstrap)

    with TestClient(app) as client:
        response = client.get("/health")

    assert response.status_code == 200
    assert calls

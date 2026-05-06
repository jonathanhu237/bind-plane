from bind_plane.api.main import create_app
from fastapi.testclient import TestClient


def test_health() -> None:
    app = create_app(bootstrap_admin=None)
    client = TestClient(app)

    response = client.get("/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}

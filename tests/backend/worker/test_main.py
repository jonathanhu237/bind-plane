from types import SimpleNamespace

from bind_plane.services import bootstrap
from bind_plane.worker import main as worker_main


def test_worker_main_does_not_run_initial_admin_bootstrap(monkeypatch) -> None:
    async def fail_bootstrap(*_args, **_kwargs) -> None:
        raise AssertionError("worker must not bootstrap initial admin")

    async def fake_reconcile_queued_jobs() -> None:
        return None

    class FakeWorker:
        def __init__(self, queues, *, connection) -> None:
            self.queues = queues
            self.connection = connection

        def work(self) -> None:
            return None

    monkeypatch.setattr(bootstrap, "bootstrap_initial_admin", fail_bootstrap)
    monkeypatch.setattr(worker_main, "get_settings", lambda: SimpleNamespace(redis_url="redis://test/0"))
    monkeypatch.setattr(worker_main, "_reconcile_queued_jobs", fake_reconcile_queued_jobs)
    monkeypatch.setattr(worker_main.Redis, "from_url", lambda _url: object())
    monkeypatch.setattr(worker_main, "Worker", FakeWorker)

    worker_main.main()

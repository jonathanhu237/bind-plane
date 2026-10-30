"""End-to-end check of a running stack started with the fake switch overlay.

    python3 e2e_check.py https://127.0.0.1:18443 root.crt
    python3 e2e_check.py http://127.0.0.1:18080

Reads SETUP_TOKEN, ADMIN_PASSWORD, FAKE_SWITCH_PASSWORD, FAKE_SWITCH_IP and COMPOSE
(the docker compose command for the stack) from the environment. It creates
assets and runs backups, compliance and one release against the fake switch only;
never point it at a stack that manages real switches.
"""

import http.cookiejar
import json
import os
import shlex
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = sys.argv[1]
CA = sys.argv[2] if len(sys.argv) > 2 else None  # Caddy's root certificate, for https only
jar = http.cookiejar.MozillaCookieJar(os.environ.get("COOKIE_JAR", "cookies.txt"))
handlers = [urllib.request.HTTPCookieProcessor(jar)]
if CA:
    handlers.append(urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=CA)))
opener = urllib.request.build_opener(*handlers)
PROFILE = {
    "name": "E2E Huawei Telnet",
    "command_templates": {
        "config_backup": "display current-configuration",
        "single_arp_query": "display arp all | include $ip",
        "arp_release": "undo arp static $ip",
        "config": "system-view",
        "exit_config": "return",
    },
    "parser_rules": {
        "arp_entry_regex": r"(?P<ip>\d+\.\d+\.\d+\.\d+)\s+(?P<mac>[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4})\s+(?P<type>static|dynamic)",
        "static_type_values": ["static"],
        "dynamic_type_values": ["dynamic"],
        "missing_patterns": ["Total:0"],
    },
    "login_prompt_patterns": {},
    "prompt_patterns": {},
    "pagination_rules": {},
    "success_patterns": [],
    "error_patterns": ["Error:"],
}
HARDENED_CONFIG = """sysname FAKE-SW
#
vlan batch 10 20
#
interface Vlanif10
 ip address 10.20.30.1 255.255.255.0
#
arp static 10.20.30.40 00e0-fc12-3456
#
user-interface vty 0 4
 authentication-mode aaa
 protocol inbound ssh
#
return
"""


def call(method, path, body=None, want=200):
    request = urllib.request.Request(
        BASE + path,
        method=method,
        data=None if body is None else json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "Accept": "application/json", "Origin": BASE},
    )
    try:
        with opener.open(request, timeout=30) as response:
            status, raw = response.status, response.read()
    except urllib.error.HTTPError as error:
        status, raw = error.code, error.read()
    if status != want:
        raise SystemExit(f"FAIL {method} {path}: HTTP {status} {raw[:300]!r}")
    return json.loads(raw) if raw else None


def wait(what, probe, timeout=120):
    deadline = time.time() + timeout
    while time.time() < deadline:
        value = probe()
        if value:
            return value
        time.sleep(2)
    raise SystemExit(f"FAIL timed out waiting for {what}")


def ok(message):
    print("ok -", message, flush=True)


def finished_task(task_id):
    task = call("GET", f"/api/ops/tasks/{task_id}")
    return task if task["queued"] + task["running"] == 0 else None


def run_task(kind):
    started = call("POST", "/api/ops/tasks", {"kind": kind, "scope": "all"}, 202)
    task = wait(f"{kind} task", lambda: finished_task(started["task_id"]))
    if task["failed"] or not task["succeeded"]:
        raise SystemExit(f"FAIL {kind}: {json.dumps(task['items'])}")
    return task


call("POST", "/api/setup", {"token": os.environ["SETUP_TOKEN"], "name": "E2E Admin", "email": "e2e-admin@example.test",
                            "password": os.environ["ADMIN_PASSWORD"], "locale": "zh-CN"}, 201)
call("POST", "/api/auth/login", {"email": "e2e-admin@example.test", "password": os.environ["ADMIN_PASSWORD"]})
jar.save(ignore_discard=True, ignore_expires=True)
ok("setup and login verified " + ("over TLS against Caddy's root CA" if CA else "over plain HTTP"))

admin = "/api/releases/admin"
credential = call("POST", f"{admin}/credentials", {"Name": "E2E credential", "Username": "netops", "Password": os.environ["FAKE_SWITCH_PASSWORD"]}, 201)["id"]
profile = call("POST", f"{admin}/profiles", PROFILE, 201)["id"]
group = call("POST", f"{admin}/groups", {"name": "Lab", "description": "Simulated switches"}, 201)["id"]
switch = call("POST", f"{admin}/switches", {"name": "FAKE-SW", "management_ip": os.environ["FAKE_SWITCH_IP"], "credential_id": credential,
                                            "command_profile_id": profile, "vendor": "huawei", "model": "S5735", "protocol": "telnet",
                                            "port": 23, "group_id": group, "site": "Lab bench"}, 201)["id"]
call("POST", f"{admin}/networks", {"switch_id": switch, "cidr": "10.20.30.0/24", "is_validated": True}, 201)
ok("credential, profile, group, switch and network created")

run_task("reachability")
first = run_task("config_backup")
assert first["changed"] == 1, first
listed = call("GET", "/api/ops/switches?q=FAKE")["items"][0]
assert listed["reachable"] is True and listed["latest_backup_id"], listed
ok("reachability check and first backup stored a configuration version")

call("POST", "/api/ops/compliance/rules", {"name": "No local password users", "pattern": r"^ local-user \S+ password",
                                           "mode": "must_not_contain", "severity": "critical"}, 201)
wait("critical compliance alert", lambda: any(a["kind"] == "compliance_violation" for a in call("GET", "/api/ops/alerts")), 60)
ok("critical compliance failure raised an alert")

compose = shlex.split(os.environ["COMPOSE"])
subprocess.run([*compose, "exec", "-T", "fake-switch", "sh", "-c", "cat > /tmp/running.cfg"], input=HARDENED_CONFIG.encode(), check=True)
second = run_task("config_backup")
assert second["changed"] == 1, second
wait("alert resolution", lambda: not any(a["kind"] == "compliance_violation" for a in call("GET", "/api/ops/alerts")), 60)
detail = call("GET", f"/api/ops/switches/{switch}")
versions = [backup["id"] for backup in detail["backups"]]
diff = call("GET", f"/api/ops/backups/{versions[0]}/diff?base={versions[1]}")["diff"]
assert diff["removed"] >= 1 and diff["added"] >= 1, diff
assert "irreversible-cipher" not in json.dumps(call("GET", "/api/ops/overview"))
ok(f"changed configuration stored version {versions[0]}, diff +{diff['added']} -{diff['removed']}, alert resolved")

prepared = call("POST", "/api/releases/prepare", {"target_ip": "10.20.30.40", "reason": "temporary_test"}, 202)
query = wait("pre-release query", lambda: (j := call("GET", f"/api/releases/jobs/{prepared['preparation_job_id']}"))["status"] not in ("queued", "running") and j)
assert query["status"] == "waiting_confirmation" and query["before_state"]["entry_type"] == "static", query
release = call("POST", "/api/releases", {"preparation_job_id": prepared["preparation_job_id"]}, 202)
done = wait("release", lambda: (j := call("GET", f"/api/releases/jobs/{release['job_id']}"))["status"] not in ("queued", "running") and j)
assert done["status"] == "succeeded" and done["after_state"]["entry_type"] == "dynamic", done
ok(f"release of 10.20.30.40 on {done['switch_name']} ({done['switch_management_ip']}) succeeded; entry is now dynamic")

call("POST", "/api/ops/schedules", {"name": "E2E every minute", "kind": "reachability", "cron": "* * * * *", "scope": "all"}, 201)
wait("scheduled run", lambda: any(t["schedule_name"] == "E2E every minute" for t in call("GET", "/api/ops/tasks")), 100)
ok("schedule fired on its own")

events = call("GET", "/api/ops/audit?action=config")["events"]
assert {e["action"] for e in events} >= {"config.diff"}, events
assert any(e["action"] == "switch.create" for e in call("GET", "/api/ops/audit?action=switch")["events"])
ok("configuration reads and asset changes are in the audit log")
print("ALL CHECKS PASSED")

"""Verify the CLI workflow using a published-shape archive, not a source run."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.parse
import urllib.request
import zipfile


def main():
    archive = Path(sys.argv[1]).resolve()
    with tempfile.TemporaryDirectory(prefix="assist-cli-test-") as temp:
        temp = Path(temp)
        if archive.suffix == ".zip":
            with zipfile.ZipFile(archive) as z:
                z.extractall(temp)
        else:
            with tarfile.open(archive) as t:
                t.extractall(temp, filter="data")
        exe = ".exe" if os.name == "nt" else ""
        ctl = temp / ("assistctl" + exe)
        state = temp / "runtime.json"
        client = None

        def call(command, *args, expect_error=False):
            result = subprocess.run([str(ctl), command, "--state", str(state), *args], capture_output=True, text=True, encoding="utf-8", timeout=15)
            if expect_error:
                assert result.returncode != 0, result.stdout
                return
            assert result.returncode == 0, result.stderr
            return json.loads(result.stdout)

        def wait_status(session, predicate):
            deadline = time.monotonic() + 12
            while time.monotonic() < deadline:
                result = call("status", "--id", session)
                if predicate(result):
                    return result
                time.sleep(0.15)
            raise AssertionError("status deadline expired")

        try:
            first = call("up")
            assert first["ready"] and not first["reused"]
            assert call("up")["reused"]
            assert call("network")["addresses"]
            created = call("start", "--purpose", "packaged CLI validation", "--local-ip", "127.0.0.1", "--key", "start-1")
            assert created == call("start", "--purpose", "packaged CLI validation", "--local-ip", "127.0.0.1", "--key", "start-1")
            call("start", "--purpose", "different payload", "--local-ip", "127.0.0.1", "--key", "start-1", expect_error=True)
            session = created["id"]
            preview = call("preview", "--id", session)
            assert urllib.parse.urlsplit(preview["preview_url"]).hostname == "127.0.0.1"
            if "--control-only" in sys.argv:
                call("end", "--id", session)
                assert call("status", "--id", session)["revoked"]
                print("Packaged Windows CLI: startup, reuse, network, invite, idempotency, supervision and revocation passed.")
                return
            invite = urllib.parse.urlsplit(created["invite_url"])
            credentials = urllib.parse.parse_qs(invite.fragment)
            req = urllib.request.Request(f"{invite.scheme}://{invite.netloc}/api/requests/{session}", headers={"Authorization": "Bearer " + credentials["t"][0]})
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            with opener.open(req, timeout=5) as response:
                customer = json.load(response)
            client_path = temp / ("assist.exe" if os.name == "nt" else "assist-linux-amd64")
            client = subprocess.Popen([str(client_path), customer["launch_uri"]], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            connected = wait_status(session, lambda s: s["online"])
            shell = "powershell" if os.name == "nt" else "bash"
            command = "Get-Date" if os.name == "nt" else "date"
            operation = call("command", "--id", session, "--shell", shell, "--command", command, "--key", "command-1")
            assert operation["id"] == call("command", "--id", session, "--shell", shell, "--command", command, "--key", "command-1")["id"]
            completed = wait_status(session, lambda s: any(o["id"] == operation["id"] and o["status"] == "completed" for o in s["operations"]))
            result = next(o for o in completed["operations"] if o["id"] == operation["id"])
            assert result["exit_code"] == 0 and result["output"]
            slow = call("command", "--id", session, "--shell", shell, "--command", "Start-Sleep -Seconds 10" if os.name == "nt" else "sleep 10", "--key", "command-2")
            call("cancel", "--id", session, "--operation", slow["id"])
            wait_status(session, lambda s: any(o["id"] == slow["id"] and o["status"] == "cancelled" for o in s["operations"]))
            other = call("start", "--purpose", "isolation", "--local-ip", "127.0.0.1", "--key", "start-2")
            call("end", "--id", session)
            stopped = wait_status(session, lambda s: s["revoked"] and s["stopped"])
            assert not call("status", "--id", other["id"])["revoked"]
            call("end", "--id", other["id"])
            client.wait(timeout=8)
            print("Packaged CLI: startup, reuse, invite, idempotency, connection, execution, session isolation and stop receipt passed.")
        finally:
            if state.exists():
                call("down")
                deadline = time.monotonic() + 8
                while state.exists() and time.monotonic() < deadline:
                    time.sleep(0.1)
                assert not state.exists(), "runtime connection file not cleaned up"
                time.sleep(0.3)
            if client and client.poll() is None:
                client.terminate()
                client.wait(timeout=5)


if __name__ == "__main__":
    main()

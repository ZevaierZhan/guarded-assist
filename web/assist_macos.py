#!/usr/bin/env python3
"""Temporary Assist executor for Macs that already have Python 3.

The one-use pairing ticket is carried by the downloaded filename. This file
uses only the Python standard library and does not install a service.
"""

import base64
import datetime
import hashlib
import ipaddress
import json
import os
import platform
import re
import selectors
import signal
import socket
import ssl
import struct
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

VERSION = "0.2.0"
WS_MAGIC = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
FILENAME = re.compile(r"^assist--([A-Za-z0-9_-]+?)(?: \([0-9]+\))?\.py$")
ALLOW_BASH = re.compile(
    r"^\s*(?:date|pwd|whoami|uname|id|hostname|printf|echo|ls|cat|head|tail|"
    r"grep|ps|df|du|env|printenv|which|type|stat|ip|ifconfig|netstat|ss|"
    r"nslookup|dig|ping|sleep|true|false|exit)\b", re.I
)
BLOCK = [
    re.compile(p, re.I) for p in (
        r"(^|[\s;|&])(rm\s+-|rmdir\b|del\s+[/\\-]|erase\b|remove-item\b|clear-content\b|format(?:-volume)?\b|mkfs\b|diskpart\b|dd\s+if=)",
        r"(^|[\s;|&])(sudo\b|su\s+-|runas\b|net\s+user\b|useradd\b|userdel\b|passwd\b|chmod\b|chown\b|set-acl\b)",
        r"(^|[\s;|&])(shutdown\b|reboot\b|poweroff\b|restart-computer\b|stop-computer\b|sc(?:\.exe)?\s+(?:create|delete|config)\b|systemctl\s+(?:enable|disable|mask)\b)",
        r"(invoke-expression\b|\biex\b|downloadstring\b|frombase64string\b|curl\b[^\r\n|]*\|\s*(?:sh|bash|pwsh|powershell)\b|wget\b[^\r\n|]*\|\s*(?:sh|bash|pwsh|powershell)\b)",
        r"(reg(?:\.exe)?\s+(?:add|delete|import)\b|new-itemproperty\b|set-itemproperty\b|remove-itemproperty\b|netsh\s+advfirewall\b)",
    )
]


def validate_base(base):
    parsed = urllib.parse.urlsplit(base)
    if parsed.username or parsed.password or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
        raise ValueError("服务地址不能含凭据、路径或参数")
    if not parsed.hostname or parsed.scheme not in ("http", "https"):
        raise ValueError("服务地址无效")
    if parsed.scheme == "http" and parsed.hostname != "localhost":
        try:
            ip = ipaddress.ip_address(parsed.hostname)
        except ValueError as exc:
            raise ValueError("HTTP 仅允许本机或私有局域网 IP") from exc
        private_ranges = (
            ipaddress.ip_network("10.0.0.0/8"),
            ipaddress.ip_network("172.16.0.0/12"),
            ipaddress.ip_network("192.168.0.0/16"),
            ipaddress.ip_network("fc00::/7"),
        )
        if not (ip.is_loopback or any(ip in network for network in private_ranges if ip.version == network.version)):
            raise ValueError("HTTP 仅允许本机或私有局域网 IP")
    return base.rstrip("/")


def pairing_from_filename(path):
    match = FILENAME.fullmatch(os.path.basename(path))
    if not match:
        raise ValueError("请从当前邀请页下载 Python 脚本，并保留下载文件名")
    encoded = match.group(1)
    if len(encoded) > 600:
        raise ValueError("配对信息过长")
    data = json.loads(base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4)))
    if data.get("v") != 2 or not isinstance(data.get("k"), str) or len(data["k"]) != 32:
        raise ValueError("配对信息无效")
    return validate_base(data.get("b", "")), data["k"]


def request_json(base, path, token, method="GET"):
    req = urllib.request.Request(base + path, data=b"" if method == "POST" else None, method=method)
    req.add_header("Authorization", "Bearer " + token)
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            raise ValueError("拒绝认证请求跳转")

    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with opener.open(req, timeout=12) as response:
        if response.url != base + path:
            raise ValueError("拒绝认证请求跳转")
        return json.load(response)


def command_hash(msg):
    fields = [msg["id"], msg["shell"], msg["command"], msg.get("cwd", ""), msg["timeout"]]
    raw = json.dumps(fields, ensure_ascii=False, separators=(",", ":"))
    raw = raw.replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026")
    raw = raw.replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")
    return hashlib.sha256(raw.encode("utf-8")).hexdigest()


def allowed_command(msg):
    if msg.get("shell") != "bash" or not isinstance(msg.get("command"), str):
        return False
    command = msg["command"]
    cwd = msg.get("cwd", "")
    timeout = msg.get("timeout")
    if not isinstance(cwd, str) or type(timeout) is not int or not 1 <= timeout <= 60:
        return False
    if not command.strip() or not 1 <= len(command.encode("utf-8")) <= 4096:
        return False
    if "\0" in command or "\0" in cwd or len(cwd.encode("utf-8")) > 1024:
        return False
    if not isinstance(msg.get("id"), str) or not msg["id"] or msg.get("hash") != command_hash(msg):
        return False
    if any(pattern.search(command) for pattern in BLOCK):
        return False
    # The script keeps a narrower local policy than the broker for shell
    # substitution, redirects and backgrounding.
    if "$(" in command or "`" in command or ">" in command or "<" in command or "&" in command:
        return False
    return all(ALLOW_BASH.match(part) for part in re.split(r"[\r\n;]+", command) if part.strip())


class WebSocket:
    def __init__(self, endpoint, token):
        parsed = urllib.parse.urlsplit(endpoint)
        if parsed.scheme not in ("ws", "wss") or parsed.path != "/api/device":
            raise ValueError("WebSocket 地址无效")
        port = parsed.port or (443 if parsed.scheme == "wss" else 80)
        sock = socket.create_connection((parsed.hostname, port), timeout=12)
        if parsed.scheme == "wss":
            sock = ssl.create_default_context().wrap_socket(sock, server_hostname=parsed.hostname)
        sock.settimeout(45)
        key = base64.b64encode(os.urandom(16)).decode("ascii")
        host = parsed.netloc
        handshake = (
            f"GET {parsed.path} HTTP/1.1\r\nHost: {host}\r\nUpgrade: websocket\r\n"
            f"Connection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: {key}\r\n"
            f"Authorization: Bearer {token}\r\n\r\n"
        )
        sock.sendall(handshake.encode("ascii"))
        reader = sock.makefile("rb")
        status = reader.readline(4096).decode("ascii", "replace")
        headers = {}
        for _ in range(40):
            line = reader.readline(4096)
            if line in (b"\r\n", b"\n", b""):
                break
            name, _, value = line.decode("ascii", "replace").partition(":")
            headers[name.lower()] = value.strip()
        accept = base64.b64encode(hashlib.sha1((key + WS_MAGIC).encode()).digest()).decode("ascii")
        if not status.startswith("HTTP/1.1 101 ") or headers.get("sec-websocket-accept") != accept:
            reader.close()
            sock.close()
            raise ConnectionError("WebSocket 连接被服务拒绝：" + status.strip())
        self.sock = sock
        self.reader = reader
        self.lock = threading.Lock()

    def send_frame(self, opcode, payload):
        if len(payload) > 65536:
            raise ValueError("WebSocket 消息过长")
        mask = os.urandom(4)
        size = len(payload)
        header = bytes([0x80 | opcode, 0x80 | (size if size < 126 else 126 if size < 65536 else 127)])
        if size >= 65536:
            header += struct.pack("!Q", size)
        elif size >= 126:
            header += struct.pack("!H", size)
        masked = bytes(byte ^ mask[index % 4] for index, byte in enumerate(payload))
        with self.lock:
            self.sock.sendall(header + mask + masked)

    def send_json(self, message):
        self.send_frame(1, json.dumps(message, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))

    def read_exact(self, size):
        data = self.reader.read(size)
        if len(data) != size:
            raise ConnectionError("WebSocket 已断开")
        return data

    def receive_json(self):
        while True:
            first, second = self.read_exact(2)
            opcode = first & 15
            if first & 0x70 or second & 0x80:
                raise ConnectionError("WebSocket 帧无效")
            size = second & 127
            if size == 126:
                size = struct.unpack("!H", self.read_exact(2))[0]
            elif size == 127:
                size = struct.unpack("!Q", self.read_exact(8))[0]
            if size > 65536:
                raise ConnectionError("WebSocket 消息过长")
            payload = self.read_exact(size)
            if opcode == 9:
                self.send_frame(10, payload)
                continue
            if opcode == 10:
                continue
            if opcode == 8:
                raise ConnectionError("服务已关闭连接")
            if opcode != 1 or not first & 0x80:
                raise ConnectionError("不支持的 WebSocket 帧")
            return json.loads(payload)

    def close(self):
        self.reader.close()
        self.sock.close()


def run_command(ws, msg, cancel):
    start = time.monotonic()
    status, code = "failed", -1
    process = None
    emitted = 0
    try:
        if cancel.is_set():
            status = "cancelled"
            return
        ws.send_json({"type": "started", "id": msg["id"]})
        process = subprocess.Popen(
            ["/bin/bash", "--noprofile", "--norc", "-c", msg["command"]],
            cwd=msg.get("cwd") or os.path.expanduser("~"), stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True,
        )
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ, "stdout")
        selector.register(process.stderr, selectors.EVENT_READ, "stderr")
        deadline = start + msg["timeout"]
        while selector.get_map():
            if cancel.is_set() or time.monotonic() >= deadline:
                status = "cancelled" if cancel.is_set() else "timed_out"
                os.killpg(process.pid, signal.SIGKILL)
                break
            for key, _ in selector.select(timeout=0.2):
                chunk = os.read(key.fileobj.fileno(), 3000)
                if not chunk:
                    selector.unregister(key.fileobj)
                    continue
                if emitted >= 60000:
                    continue
                chunk = chunk[:60000 - emitted]
                emitted += len(chunk)
                text = chunk.decode("utf-8", "replace")
                print(text, end="", flush=True)
                ws.send_json({"type": "output", "id": msg["id"], "stream": key.data, "text": text})
        process.wait(timeout=2)
        code = process.returncode
        if status == "failed":
            status = "completed" if code == 0 else "failed"
    except Exception as exc:
        print("命令执行失败：" + str(exc), file=sys.stderr)
    finally:
        if process and process.poll() is None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=2)
            except Exception:
                pass
        try:
            ws.send_json({"type": "result", "id": msg["id"], "status": status,
                          "exit_code": code, "duration_ms": int((time.monotonic() - start) * 1000)})
        except OSError:
            pass


def main():
    if sys.version_info < (3, 8):
        raise RuntimeError("需要 Python 3.8 或更新版本")
    base, ticket = pairing_from_filename(sys.argv[0])
    grant = request_json(base, "/api/bootstrap", ticket)
    if grant.get("scope") != "policy-shell-v3" or not grant.get("request_id"):
        raise ValueError("授权类型无效")
    expires = datetime.datetime.fromisoformat(grant["expires"].replace("Z", "+00:00"))
    if expires <= datetime.datetime.now(datetime.timezone.utc):
        raise ValueError("授权已过期")
    paired = request_json(base, "/api/pair", ticket, "POST")
    if (paired.get("request_id") != grant["request_id"] or paired.get("scope") != grant["scope"]
            or paired.get("expires") != grant["expires"]):
        raise ValueError("配对权限不一致")
    ws_url = base.replace("https://", "wss://", 1).replace("http://", "ws://", 1) + "/api/device"
    if paired.get("websocket") != ws_url or len(paired.get("device_token", "")) != 32:
        raise ValueError("设备凭据无效")
    ws = WebSocket(ws_url, paired["device_token"])
    active = None
    cancel = None
    seen = set()
    try:
        ws.send_json({"type": "hello", "host": socket.gethostname(), "os": "darwin/" + platform.machine(),
                      "version": VERSION, "pid": os.getpid(), "protocol_registered": False,
                      "shells": ["bash"], "cwd": os.path.expanduser("~")})
        print("assist 已连接 " + base + "；按 Ctrl+C 可结束协助。")
        while True:
            msg = ws.receive_json()
            if active and msg.get("type") == "exec":
                worker.join(timeout=0.2)
            if active and not worker.is_alive():
                active, cancel = None, None
            kind = msg.get("type")
            if kind == "stop":
                break
            if kind == "cancel":
                if active and msg.get("id") == active and cancel:
                    cancel.set()
                continue
            if kind != "exec":
                continue
            op_id = msg.get("id", "")
            if active or op_id in seen or len(seen) >= 30 or not allowed_command(msg):
                ws.send_json({"type": "blocked", "id": op_id, "text": "本机策略或命令摘要拒绝"})
                continue
            seen.add(op_id)
            active = op_id
            cancel = threading.Event()
            worker = threading.Thread(target=run_command, args=(ws, msg, cancel), daemon=True)
            worker.start()
    except KeyboardInterrupt:
        print("正在停止协助…")
    finally:
        if cancel:
            cancel.set()
        if active:
            worker.join(timeout=5)
        try:
            ws.send_json({"type": "stopped"})
        except OSError:
            pass
        ws.close()


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print("assist 未连接：" + str(error), file=sys.stderr)
        sys.exit(1)

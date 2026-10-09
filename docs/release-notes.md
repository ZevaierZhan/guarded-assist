Guarded Assist: temporary remote assistance driven by a local CLI and an AI skill.

Download `guarded-assist-server-<os>-<arch>` for the helper's machine. The bundle includes `assist-server`, `assistctl`, and the native clients served by invitation pages. The helper needs no Go, Python, Node, or MCP registration.

Download `guarded-assist-skill.zip` to install the `guarded-assist` skill. Its bootstrap scripts download a pinned Release and verify `SHA256SUMS` before extraction.

Native client binaries are also available separately. Windows invitations download an EXE; macOS invitations package both Mac architectures into a request-bound App and retain the Python fallback. Clients have not been signed or notarized.

Start with `assistctl up`, create an invitation with `assistctl start --purpose "diagnose the reported issue" --key <unique-key>`, then use `status`, `preview`, `command`, `cancel`, and `end`. Use `down` only when all sessions in that runtime may be disconnected.

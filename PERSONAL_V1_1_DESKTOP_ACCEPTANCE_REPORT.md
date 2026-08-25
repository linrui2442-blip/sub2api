# Sub2API Personal Edition v1.1 Desktop Acceptance Report

## Candidate

- Branch: `personal-v1.1-desktop`
- Source commit: `3a4eb9309dc34d4319c1b3d37a3a2ef725cc32c2`
- Version: `1.1.0-rc.1`
- Build type: `personal`
- Windows candidate: `sub2api-personal-v1.1-rc.exe`
- File size: `79,386,112` bytes
- SHA256: `203DD37CDC8A328112FF3912A2358183FE9D96C492A193381DFC1DE4A395FF34`

This candidate has not replaced the published v1.0.1 executable or the desktop `Sub2API Personal v1.0.1` archive.

## Architecture boundary

The desktop implementation is a Windows tray lifecycle shell over the existing Gateway application graph. No second Gateway, OAuth system, token store, database, account pool, scheduler, or API-key router was introduced.

## Implemented desktop capabilities

- Native Windows notification-area icon
- Background Gateway operation without a persistent console window
- Open management page
- Start, stop, and restart Gateway
- Gateway running/stopped status in the tray
- Open the durable Personal log directory
- Per-user Windows startup toggle
- Graceful exit
- Single desktop instance guard

## Automated validation

- `go test ./...`: PASS
- Focused `./cmd/server ./internal/personal` tests: PASS
- Frontend typecheck: PASS
- Frontend production build: PASS
- Windows AMD64 embedded build: PASS
- `git diff --check`: PASS
- GitHub CI at candidate source commit: PASS
- Personal Edition CI at candidate source commit: PASS
- Security Scan at candidate source commit: PASS

## Windows runtime validation

- Candidate launched from its exact RC path: PASS
- Management endpoint `http://127.0.0.1:8080/`: HTTP 200
- Second executable launch exits without creating another instance: PASS
- Stop Gateway releases port 8080: PASS
- Restart restores HTTP 200: PASS
- Restart does not create a duplicate process or port conflict: PASS
- Existing Personal data directory reused: PASS
- Windows startup entry remained disabled during runtime testing: PASS

## Persistent data verification

Read-only SQLite counts before and after the desktop runtime checks were unchanged:

- Owner/users: 1
- Accounts: 5
- API keys: 2
- Groups: 7
- Usage records: 28

No OAuth reauthorization, database reset, or API-key replacement was performed.

## Remaining manual release checks

The following checks are intentionally not claimed as completed by automation:

- Physical tray-menu click-through and notification appearance on the user's desktop
- Real Windows sign-out/reboot startup test
- Installer, upgrade, and uninstall acceptance

## Conclusion

The portable v1.1 Desktop RC is ready for manual tray interaction and Windows reboot acceptance. It is not yet declared the formal v1.1 release, and it does not replace v1.0.1.

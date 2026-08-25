# Sub2API Personal Edition v1.1 Desktop

v1.1 Desktop is a thin Windows desktop shell around the existing Personal Gateway. It does not replace or redesign the Gateway, Provider, OAuth, SQLite, Account Pool, Scheduler, API Key, Usage, or Audit architecture.

## Starting the application

Run the Windows desktop executable. The Gateway starts in the background and the Sub2API icon appears in the Windows notification area. The executable uses the existing Personal data directory:

`%LOCALAPPDATA%\Sub2 Personal`

Only one desktop instance can run at a time. Starting the executable again does not create a second Gateway or bind a second listener.

## Tray controls

Right-click the Sub2API tray icon to access:

- Gateway status
- Open management page
- Start Gateway or Stop Gateway
- Restart Gateway
- View logs
- Enable or disable startup with Windows
- Exit Sub2API

Double-clicking the tray icon opens the management page. Closing the browser does not stop the Gateway.

`Stop Gateway` releases port 8080 but leaves the tray shell running, so the Gateway can be started again from the same menu. `Exit Sub2API` stops the Gateway and closes the tray process.

## Logs and local data

The log directory is:

`%LOCALAPPDATA%\Sub2 Personal\logs`

The tray's `View logs` command opens that directory in Windows Explorer.

Do not delete `%LOCALAPPDATA%\Sub2 Personal` when replacing the executable. It contains the live SQLite database and Personal runtime data, including accounts, OAuth credentials, API keys, groups, usage history, and Owner initialization.

## Startup with Windows

The startup option writes only the current executable path to the current user's Windows startup registry entry. It does not install a Windows service. Move the executable to its permanent location before enabling this option.

## Release boundary

The current v1.1 candidate is a portable Windows executable. Installer, upgrade/uninstall packaging, and a real Windows reboot acceptance remain separate release checks. The published v1.0.1 archive and tag remain unchanged.

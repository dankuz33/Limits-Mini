# Connect Claude to Limits Mini

[Русский](claude-setup.ru.md) · [Home](../README.md)

For Limits Mini 0.2.0. These steps use Claude Code installed natively on Windows, not inside WSL.

## Before you start

You need an account with subscription access to Claude Code and connectivity to Anthropic's services. Signing in only through Claude Desktop or a browser is not sufficient for this implementation: Limits Mini reads Claude Code CLI authentication.

You do not need to purchase API credits for the widget. An Anthropic Console API-key login does not replace a subscription sign-in for these counters.

## 1. Install Claude Code

Open a regular **PowerShell** window, without running it as administrator:

```powershell
winget install --id Anthropic.ClaudeCode --exact
```

Close the terminal completely, open a new one and check the installation:

```powershell
claude --version
```

You should see a Claude Code version number. If `winget` is unavailable, use another method from the [official installation guide](https://code.claude.com/docs/en/setup).

## 2. Sign in to Claude

In PowerShell, run:

```powershell
claude auth login
```

Complete browser sign-in using the account whose limits you want to display. If asked to choose a login method, select **Claude account with subscription**, not **Anthropic Console account**.

You do not need to start an agent session in a project folder or trust `C:\Windows\System32`. The `claude auth login` command starts authentication specifically.

Check the result:

```powershell
claude auth status --text
```

It should report a signed-in account. Review the output for personal information before sharing it.

The sign-in and status commands are documented in the [official CLI reference](https://code.claude.com/docs/en/cli-reference).

## 3. Refresh the widget

Return to Limits Mini and select **right-click → Обновить сейчас**, meaning **Refresh now**.

If it still shows `--`, select **Лимиты и состояние подключения**, meaning **Limits and connection status**. This explains whether the login is missing, the token has expired, the network is unavailable or the service rejected the request.

Once the CLI is installed, you can also start sign-in from **Подключение → Подключить Claude Code**, meaning **Connection → Connect Claude Code**. Version 0.2.0 does not install the CLI automatically from this menu.

You do not need to keep the Claude Code window open while its saved access token remains valid. Limits Mini does not refresh that token itself. If it expires, open Claude Code. If the widget does not recover, repeat `claude auth login` and refresh the widget.

## When a proxy is needed

Use this section only for connectivity problems such as `Unable to connect to Anthropic services`. No additional settings are needed when the connection already works.

A working browser does not prove that the CLI uses the same network route. Claude Code supports [HTTP_PROXY and HTTPS_PROXY](https://code.claude.com/docs/en/network-config).

For a **local HTTP proxy**, an example is:

```powershell
$proxy = "http://127.0.0.1:10809"
$env:HTTP_PROXY = $proxy
$env:HTTPS_PROXY = $proxy
claude auth login
```

`10809` is only an example. Replace it with the HTTP port shown by your trusted proxy client, and keep that client running. Do not substitute a SOCKS port for this setup or disable TLS certificate validation.

These commands affect only this PowerShell session and processes launched from it. They do not permanently change Windows proxy settings or update processes that are already running.

**For Limits Mini itself:** version 0.2.0 reads a static Windows system proxy when it starts. Enable that proxy in your client, then fully restart Limits Mini. TUN is not required for an explicit HTTP proxy connection; the current app does not evaluate PAC scripts.

Alternatively, close the existing widget using **Выход**, meaning **Exit**, and launch the installed utility from the same PowerShell session:

```powershell
Start-Process "$env:LOCALAPPDATA\LimitsMini\LimitsMini.exe"
```

The new process inherits the proxy environment. For a non-installed copy, use the actual path to its EXE.

<details>
<summary>Test Anthropic connectivity through the proxy</summary>

In the same PowerShell session:

```powershell
curl.exe --proxy $proxy --connect-timeout 10 --max-time 20 --head https://api.anthropic.com
```

An initial `HTTP/1.1 200 Connection established` can be the proxy's response to the tunnel request. Also check the subsequent HTTP response from the destination.

For example, a final `404 Not Found` when requesting the site's root confirms HTTPS connectivity. It does not validate your account or access to the usage endpoint. If the connection times out, restore network access first.

</details>

## Common issues

**“Claude Code CLI not found.”** Check `claude --version` in a new terminal. Make sure you installed the CLI, not only Desktop. Restart Limits Mini after installing it.

**Signed in, but the widget cannot find the login.** Check the account and login type with `claude auth status --text`. The standard Windows credential file is `%USERPROFILE%\.claude\.credentials.json`. For a custom location, use **Подключение → Claude: выбрать .credentials.json** to select the file locally. Do not paste its contents into a chat or Issue. Storage locations are documented by [Anthropic](https://code.claude.com/docs/en/authentication).

**“Token expired” or HTTP 401.** Open Claude Code; run `claude auth login` if needed. Refresh the widget after sign-in completes.

**HTTP 403.** The service refused access; the status alone does not identify a single cause. Check the account, permissions and service availability on your network.

**HTTP 429.** Requests are being rate-limited. Wait for the pause to end instead of repeatedly clicking refresh. This error alone does not mean your weekly model allowance is exhausted.

**Percentages differ from the Usage page.** Compare the same account and quota window, and refresh both views. In remaining mode, the widget shows `100 − used`. The values may have been fetched at different times.

> Never commit `.credentials.json`, `auth.json` or tokens to a public repository. Do not disable antivirus protection or HTTPS validation to connect.

# Telegram TUI

A terminal client for one personal Telegram account, written in Go. It uses MTProto via `gotd/td` and Bubble Tea v2. The current implementation supports sign-in, a chat list, message history, incoming messages, text sending, search within a chat, and read receipts. Only text content is rendered; media appears as a placeholder.

## Requirements

- Go 1.27.1 or newer in the Go 1.27 series
- macOS or Linux and an interactive terminal
- Telegram application `api_id` and `api_hash` from [my.telegram.org](https://my.telegram.org)

## Run

Build once, then edit `dist/config.json` using the format in [config.example.json](config.example.json). Put your application `api_id` and `api_hash` there. `phone` is optional; when blank, the app prompts for a number. Keep this file private (`chmod 600 dist/config.json`). The `dist/` directory is ignored by Git.

```sh
go build -o dist/telegram-tui ./cmd/telegram-tui
./dist/telegram-tui
```

The binary reads `config.json` next to itself, even when launched from another directory. Use `--config /path/to/config.json` to select another file. The first run asks you to **create and confirm a local session passphrase**, then asks for a phone number if it is absent from the config, a Telegram sign-in code, and the Telegram 2FA password when needed. Later runs need that same local passphrase. It is separate from the Telegram code and 2FA password. The app credentials belong to the Telegram application; the account itself still signs in by phone. Telegram may deliver the sign-in code inside an existing Telegram app rather than by SMS.

The encrypted MTProto session is stored at `~/Library/Application Support/telegram-tui/session.enc` on macOS or under `$XDG_CONFIG_HOME/telegram-tui/session.enc` on Linux (default `~/.config/telegram-tui/session.enc`). It has mode `0600`. If you forgot the local passphrase or the session is damaged, run `./dist/telegram-tui --reset-session`. After you type `RESET`, the old encrypted file is moved to a uniquely named backup in the same directory; the app then asks for a new local passphrase and signs in by phone. You can also move `session.enc` aside manually. Neither method changes a Python/Telethon session.

If Telegram returns `PHONE_CODE_INVALID`, start the app again and enter the newest code from the delivery channel named in the prompt. A code from an earlier attempt will not work. Do not use `--reset-session` for this error, and do not share the code with anyone.

## Keys

| Screen | Keys |
| --- | --- |
| Chat list | `j`/`k` or arrows: move; Enter: open; `r`: refresh; `q`: quit |
| Chat | `i`: compose; `/`: search; `u`: fetch older messages; PgUp/PgDn: scroll; `r`: refresh; Esc: chat list |
| Compose/search | Enter: submit; Esc: cancel |
| Search results | `j`/`k`: move; `u`: older results; Enter: show result with context; Esc: chat |

`Ctrl+C` exits from any screen. A failed send leaves the text in the composer. Inspect the chat before manually retrying because a network timeout cannot prove whether Telegram accepted the message.

To log out, run `./dist/telegram-tui --logout`. The command requires a typed `yes` confirmation and your session passphrase. It asks Telegram to revoke the session, then removes the local encrypted file only after Telegram confirms success.

## Security and limitations

Session encryption uses Argon2id and XChaCha20-Poly1305 with a fresh salt and nonce on every write, and an atomic file replacement. The passphrase is never saved. History is held only in process memory. No telemetry or server is included.

This is an early client. Dialogs and search results are loaded in pages. A gotd update manager attempts in-process gap recovery; after reconnect the client refreshes the chat list and open chat, and after restart it loads them again. The update manager's internal state is not persisted across launches, so a long offline gap has not been validated against a live account. Search result selection requests nearby history. No media transfer, calls, edit/delete, reactions, multiple accounts, or offline cache are implemented. The authentication flow does not accept new Terms of Service or create accounts.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o dist/telegram-tui ./cmd/telegram-tui
```

See [the implementation plan](docs/telegram-tui-plan.md) and [current work tracking](tasks/todo.md). No automated test needs Telegram credentials.

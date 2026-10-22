# Telegram TUI: agent-led implementation plan

## Product goal

Build an interactive terminal client for one personal Telegram account on macOS and Linux. The first release supports sign-in with a Telegram code and optional 2FA password; private chats, groups, and channels; a chat list and unread counts; paginated message history; live incoming text; text sending where permitted; search within the open chat; and marking a chat read when opened.

Media, files, calls, reactions, editing, deleting, multiple accounts, and offline message history are outside v1. The client requires a network connection to load chats and history.

## Technical decisions

- Use Go 1.27 in a module rooted in `console_telegram`. Verify and pin compatible stable dependencies in `go.mod` and `go.sum`.
- Use `github.com/gotd/td` for a personal-account MTProto connection. Evaluate its update and gap-handling facilities before adding custom synchronization logic.
- Use Bubble Tea v2, Bubbles v2, and Lip Gloss v2 for the TUI. Keep network calls outside the view/update logic.
- Separate the Telegram adapter, application state, and TUI behind a small interface so interface tests need no live account.
- Read `api_id` and `api_hash` from a private `config.json` next to the binary; an optional phone number can also be stored there. Never log or commit real credentials, login codes, passwords, or sessions.
- Encrypt the local session using a user-entered passphrase, Argon2id, and XChaCha20-Poly1305. Store it in the user data directory with mode `0600` and atomic replacement. A wrong passphrase must not destroy the saved session.
- Keep message history only in process memory. Reopen the encrypted session and fetch chats and history after restarting.
- Show connecting, reconnecting, loading, and failure states. Do not automatically retry a send when the result of the previous attempt is unknown.

## Agent workflow

For every iteration, run: architect -> implementer -> coder -> tester -> reviewer -> fixes -> repeat verification -> acceptance.

| Role | Responsibility | Handoff |
| --- | --- | --- |
| Architect | Define behavior, interfaces, risks, and acceptance criteria. | Bounded implementation spec. |
| Implementer | Split work into owned tasks and integrate against the spec. | Coding tasks and integration review. |
| Coder | Write one scoped section of code and run local checks. | Changed files and check results. |
| Tester | Add meaningful tests and check regressions. | Reproducible pass/fail report. |
| Reviewer | Check correctness, security, failure behavior, and release readiness. | Findings or sign-off. |

Only one agent may write application code in the shared worktree at a time. The parent agent owns decisions and accepts each iteration. Agents do not commit, push, deploy, release, or modify external systems on their own. If implementation diverges from the accepted specification, return to architecture before proceeding. Track progress and evidence in `tasks/todo.md`. Update a topic-based Mermaid file in `docs/diagrams/` after each iteration that changes runtime flow.

## Iterations and gates

### 0. Project foundation

Create the module, package boundaries, configuration, README, build commands, and CI checks for macOS and Linux with explicit timeouts. Verify dependency compatibility and pin versions. **Gate:** clean build, tests, vet, and formatting checks run without Telegram credentials.

### 1. Authentication and session

Implement code and optional 2FA login, encrypted session save and restore, wrong-password handling, and explicit logout. **Gate:** a restored session works after restart; wrong passphrase leaves the file intact; no secrets appear in logs or tracked files.

### 2. Chat list and navigation

Load private chats, groups, and channels with unread counts. Add keyboard navigation and loading, empty, and disconnected states. **Gate:** terminal resize and connection errors do not crash the app.

### 3. History and updates

Load history in pages, display live messages, update unread counts, mark a chat read on opening, and recover after reconnection. Use gotd's existing update mechanisms where suitable. **Gate:** tested flows show no omitted or duplicated messages.

### 4. Text sending

Add compose, sending state, successful result, permission errors, and explicit retry. **Gate:** uncertain network outcomes cannot cause automatic duplicate sends.

### 5. Search in open chat

Query Telegram, show results, navigate to a selected message, and handle empty results or errors. **Gate:** navigation works for private chats, groups, and channels within account permissions.

### 6. Stabilization

Handle shutdown, document installation and login, check key bindings and terminal behavior, build binaries, and perform a manual test with a dedicated account. Prepare release artifacts without publishing them. **Gate:** automated checks pass, manual results are recorded accurately, limitations are documented, and reviewer findings are resolved.

## Verification

- Unit tests: session encryption, invalid inputs, state transitions, pagination, and send outcomes.
- Adapter tests with a fake Telegram backend: login states, chats, history, search, reconnect, and errors.
- Checks: `go test ./...`, `go test -race ./...`, `go vet ./...`, formatting, macOS and Linux builds, and a secret scan of changed files.
- Manual account test: login, optional 2FA, private chat, group, channel, network interruption, and restart. Report it as unverified until it actually runs.

## Assumptions and review policy

The app is local with no server or telemetry. Users provide their own Telegram application credentials once in a private configuration file; account sign-in still uses a phone number and Telegram code. Dependency versions are selected and pinned after a compatibility check in iteration 0. The plan may be revised after review, with the reason noted here and the resulting work updated in `tasks/todo.md`.

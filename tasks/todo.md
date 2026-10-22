# Telegram TUI implementation

Plan: [docs/telegram-tui-plan.md](../docs/telegram-tui-plan.md)

## Progress

- [x] Save the approved plan in Markdown.
- [x] Complete read-only architecture review.
- [x] Establish Go module, pinned dependencies, README, and CI.
- [x] Implement encrypted session and phone/code/2FA authentication.
- [x] Implement paginated chat list and history, in-process update recovery, reconnect refresh, and read receipts.
- [x] Implement text sending, paginated chat search, context fetch, and confirmed logout.
- [x] Add focused tests and run tests, vet, race, and macOS/Linux cross-builds.
- [x] Complete security and release review; resolve findings from the review.
- [x] Verify private config file and packaged binary workflow.
- [x] Verify safe recovery from an unknown local session passphrase, code prompts, and rebuild the binary.
- [ ] Record manual Telegram-account verification when available.

## Review notes

- No live Telegram credentials or test account have been provided. Automated work must use fakes and avoid claiming live verification.
- Update manager state is memory-only. The client refetches chat list and recent open-chat history after reconnect and launch; long offline gap recovery remains unverified.
- Terms of Service acceptance and new account creation remain unimplemented.
- The application now reads credentials from a private config file beside the binary. This changes packaging, not Telegram's requirement for application credentials or account sign-in by phone.
- A user hit `invalid passphrase or damaged session` before any Telegram connection. The local session passphrase was not known; a confirmed backup-and-reset flow is being added so the encrypted file need not be deleted.
- A later sign-in reached Telegram but Telegram rejected the submitted code with `PHONE_CODE_INVALID`. The delivery channel now appears in the prompt and surrounding whitespace is trimmed; the actual code and delivery cannot be verified without the user's new attempt.
- The session-backup and code-channel tests, full Go tests with race detection, vet, native macOS arm64 build, Linux amd64 cross-build, and source secret scans passed. Live sign-in still requires the user's fresh code.
- Built a native macOS arm64 binary in `dist/telegram-tui` and a Linux amd64 binary in `dist/telegram-tui-linux-amd64`. `dist/config.json` is a private placeholder with mode `0600`; a smoke run found it and stopped before connecting because its credentials are blank.
- Final review found no remaining high-severity issues. Update backpressure, service-message pagination, affected-history handling, and stale TUI responses were corrected and covered where feasible by regression tests.

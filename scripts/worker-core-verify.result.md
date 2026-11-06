**Findings**
- **Retry after revocation/unshare never terminates.** `receiveFileRetry` rejects `Revoked` and unshared retries (`internal/core/file_status.go:97`, `:104`), but `receive` returns before recording Seen or sending an ack (`internal/core/protocol.go:351`). The recipient's durable `file_retry` remains queued and replays every discovery tick. Repro: A→B send; B receives offer; A cancels; B scans `cancelled` then calls `RetryTransfer`; B remains `retry_requested`, B outbox retains one `file_retry`, and A repeatedly reports `发送方已取消，请等待重新发送` — no terminal status returns to B. Same path after `RemoveShare`. Rejection should ack and send a terminal `file_status`.
- **A replaced ownership offer can orphan an accepted invitation and block Leave.** The new guard blocks Leave for any accepted ownership invite from the current owner (`internal/core/groups.go:296-300`). `TransferOwner` deletes prior offers without notifying their targets (`:351-354`), and a later `ownership_accept` with no matching offer is silently ignored (`:536-537`); no `ownership_result` is sent. Repro: group A/B/C; A→B transfer; B accepts while A is offline, then B closes; after A restarts and reassigns to C (B's old offer forced past 5 minutes so activeOwnership is false), C rejects; B reconnects — its queued accept is consumed with `OwnerOffers=0`, its invite stays `accepted`, and `LeaveGroup` returns `群主转让确认中，请等待完成后再退出`. Old targets need an expiry result when offers are replaced, or stale accepted invites must not block Leave.

**Verified**
- Prefix guard covers `group_state` install, invite/offer install, and proof adoption; normal completion and owner-expiry reconciliation pass.
- `serveFile` revocation, ordered flush/tombstones/atomic cancel, and startup plus manual offline installed-file recovery pass.
- Ran the 12 focused tests and full `go test ./internal/core -count=1`; both pass. Overlay-only repros; no repo edits, commits, or windows.

**Source**
Reviewed `bccd99a` (2026-11-07T02:52:48+08:00); core hashes were unchanged through `d970251` (03:00:58). `git diff bccd99a..HEAD -- internal/core internal/domain` is empty and the core tree stayed clean during review.

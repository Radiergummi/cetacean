# Persisting DCR registrations

**Pull request:** [#207](https://github.com/Radiergummi/cetacean/pull/207)
**Depends on:** [#74](https://github.com/Radiergummi/cetacean/issues/74) (refresh
token persistence), [#160](https://github.com/Radiergummi/cetacean/issues/160)
(consent records)
**Status:** implemented

Carry dynamically registered MCP clients across a restart, so a client that
registered under RFC 7591 comes back to a server that still knows its
`client_id`.

## What existed before

Refresh tokens and consent records were already written to
`<data_dir>/mcp-tokens.json`. The DCR registry was not: it lived in memory for
the lifetime of the process. A restart therefore left every dynamically
registered client holding a `client_id` the server no longer recognised, and it
had to register and be approved again before it could reconnect. The consent
record that *had* survived was stranded along with it, because a `ConsentKey`
names the client by id.

## Decisions

### The registrations ride in the existing file, at the existing version

The format version stays at **2**. The key is additive and reads in both
directions, and bumping it would make a rollback discard the whole file — tokens
and consent included — rather than only the key the older build cannot use. A
file written before this change loads and yields no registrations, which is what
deployments have today: each DCR client registers once more, and then persists.

### The snapshot keeps the registry's own eviction order rather than sorting

That order is state, not presentation — it decides which client the next
registration at capacity drops. It also only grows at the tail, so it is stable
across unrelated writes: a token rotation does not rewrite the file with the
clients reshuffled.

### `Restore` truncates to the configured cap, keeping the newest

The cap is read from config at every start, so a file written under a larger
`mcp.oauth.dcr_max_clients` must not restore over the current one. Truncating
from the head drops exactly the clients the next registration would have
evicted.

### No TTL on a registration

Everything else in this file is pruned on restore — expired tokens, lapsed
consent. RFC 7591 gives a registration no expiry, so none was invented. The cap
is the bound, now a durable one rather than a per-process one.

### Nothing is re-validated on load

A registration is not a credential: these are public clients, and anyone may
mint one at the registration endpoint. But a forged redirect URI in a
hand-edited file would now outlive a restart. That is the same integrity
property the consent records in this file already carry, under the same mode
`0600`.

What `Restore` *does* enforce is its own invariant: a repeated `client_id` is
ignored. The registry keeps a map and an eviction order, and a file that named
one id twice would leave the order longer than the map — the cap would then
under-count, and an eviction would delete an id a later order entry still names,
dropping a client that is not the oldest.

### A nil registry is the DCR-disabled case

`Snapshot` and `Restore` tolerate it, so the state file does not have to learn
what a disabled registration endpoint means. Registrations the server would
refuse to resolve do not outlive the restart in the file.

## Bounding what a registration can store

Persistence changes the cost of an oversized registration. `/oauth/register` is
unauthenticated, and the whole state file is rewritten and fsynced on every
later token or consent change — so a large record is paid for once per
registration, and then again on every unrelated write for as long as it is kept.

The 64 KiB body cap bounds the request, not the record. `client_name`, a single
redirect URI, or the `redirect_uris` array could each fill it; at the default
cap of 1000 clients that is tens of megabytes in front of every subsequent
write. Each field is therefore bounded on its own: 256 bytes of `client_name`,
10 redirect URIs, 2048 bytes each.

`grant_types` and `response_types` needed no new limit, only an honest reading
of the one they had. Both must be subsets of a fixed set, and a subset admits no
repeats — but the validation checked membership per entry, so ten thousand
copies of `code` passed. Rejecting a repeat bounds each field to the size of the
set it is drawn from.

These are rejections, not truncations: RFC 7591 has an error for metadata the
server will not accept, and a client that is silently given something other than
what it registered is worse off than one told no.

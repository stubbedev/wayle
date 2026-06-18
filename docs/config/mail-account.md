---
title: mail-account
outline: [2, 3]
---

# mail-account

<div v-pre>

One mail account in the `[modules.mail]` per-account breakdown.

Each account has its own notmuch query; the dropdown shows the per-account
unread counts and the bar shows their sum.

## Example

```toml
[[modules.mail.accounts]]
name = "Work"
query = "folder:work/INBOX and tag:unread"
provider = "gmail"
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | required | Display name shown in the dropdown. |
| `query` | string | required | notmuch query whose match count is this account's unread total. |
| `provider` | [`MailProvider`](/config/types#mail-provider) | `"generic"` | Provider, selecting the default brand icon. |
| `icon` | unknown | `null` | Optional icon override. Empty uses the provider's default icon. |

## Default configuration

Required fields (must be set in your config): `name`, `query`.

```toml
[[modules.mail-account]]
provider = "generic"
```


</div>

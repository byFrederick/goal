---
title: Connect GitHub
description: Send GitHub events to Goal with one organization webhook, give agents GitHub tools without an app, and add a GitHub Enterprise or self-managed GitLab host.
---

You end with GitHub events reaching your factories, agents that can use GitHub's tools with read-only or write tokens, and, if you need one, a code host of your own on the organization's list. Goal needs no GitHub App: webhooks and fine-grained tokens do the work.

## Send events with one organization webhook

An owner of the GitHub organization adds one organization webhook in GitHub. It covers every repository of the organization, including ones created later, so it is added once, not per repository or per factory.

1. In Goal, an organization owner or admin creates the webhook: `goal webhooks create github-acme --sender github`. It prints the secret's value once; copy it. The address is `POST /hooks/<webhook id>` on goald's public origin. [Receive events with organization webhooks](../webhooks/#create-a-webhook) has the details.
2. In GitHub, an owner of `acme` opens the organization's Settings, Webhooks, Add webhook. Set the payload URL to the address, the content type to `application/json`, and the secret to the value from step 1. Choose the events your triggers use, listed as `setup_events` in `goal webhooks list --json`.
3. In Goal, open the webhook to the factories that use it, optionally limited to some repositories: `goal webhooks open github-acme acme-line --repos acme/api,acme/web`.

A factory editor sees a delivery's evaluation by their factory and never its body or headers. A person on a team with read-only or no GitHub access does not need to set anything up: ask an owner of the GitHub organization and an owner or admin of Goal's organization.

Triggers then name the sender and event, such as `github.check_run`. A trigger can name the item's repositories from the event with `create.repositories: ["{{repo}}"]`, and an event from a repository the factory doesn't list is skipped: see [Repositories of an item](../intake/#repositories-of-an-item).

## Use repository webhooks on a personal account

A personal GitHub account has no organization webhook. Create one repository webhook per repository, all with the same address and the same secret as one Goal webhook:

1. Create the Goal webhook once, as above.
2. In each repository, Settings, Webhooks, Add webhook, with the same payload URL and secret.
3. Open the Goal webhook to the factory. Add `--repos` with each repository's `owner/name` if you want to limit it, for example `--repos acme/api`.

GitHub's ping, sent when you add a webhook, is accepted and shown as "Received GitHub's ping".

## Give agents GitHub tools

Webhooks bring events in. For what git doesn't do, such as reading issues, checks and pull request comments, or searching code across repositories, agents use GitHub tools in their environment. Two templates in the tool catalog set them up, and [Access and secrets](/docs/concepts/access-and-secrets/#environment-files) describes their parameters:

- **`github-read`** runs GitHub's MCP server in read-only mode and suggests a read-only fine-grained token. Use it for agents that only read, and for checks.
- **`github`** keeps every tool, with merges and deletes asking a person first, and suggests a token that can write.

Publishing warns when an agent that only reads, or a check, can call a GitHub tool that writes. Switch the environment to `github-read`, or set those tools to `ask` or `off`. A token an environment names, such as `GH_TOKEN`, is the environment's: Goal can't narrow it, so make it read-only too. Pick the token the way [Connect code to a factory](../connect-code/#pick-a-token-for-each-entry) describes.

## Add a code host for Enterprise or self-managed GitLab

github.com and gitlab.com are built in. A repository on any other host, such as `ghe.example.com` (GitHub Enterprise) or `gitlab.example.com` (self-managed GitLab), needs its host on the organization's list before a version that uses it publishes or its token is tested.

An owner adds it in a session signed in within the last 10 minutes, with the host and its kind. The list is replaced as a whole, so include the hosts already there:

```sh
curl -X PUT https://goal.example.com/v1/orgs/acme/code-hosts \
  -H 'Content-Type: application/json' \
  -d '{ "hosts": [{ "host": "ghe.example.com", "kind": "github" }, { "host": "gitlab.example.com", "kind": "gitlab" }] }'
```

Send the request with the signed-in session's cookie; API keys and personal tokens can't set the list. The host must be reachable from the internet. The rules, the derived API bases and the errors are in [Code hosts](/docs/getting-started/organizations/#code-hosts). The Code hosts section of organization settings is not on every installation yet, so the API is the path described here.

Then list its repositories as usual in `runtime.repositories`, with `https://ghe.example.com/acme/app` as the `url`. For GitHub tools on an Enterprise host, pass `host: ghe.example.com` to the template.

## Next

- [Connect code to a factory](../connect-code/): the repositories, tokens and access a factory's agents use.
- [Receive events with organization webhooks](../webhooks/): the delivery log and every sender.

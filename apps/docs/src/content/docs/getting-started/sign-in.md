---

title: Sign in
description: How to sign in to the Goal web app and the goal CLI, and what to do when you are not invited.
sidebar:
  order: 2
---

You sign in with a code sent to your work email. The CLI signs in through the web app: it shows a code, and you approve that code in your browser. Goal is invite-only, so your email needs an invitation before the first sign-in works.

## Before you start

Ask an owner of your organization to invite you. The invitation goes to your email address and expires after 72 hours. If it expires, ask for a new one.

On a new Goal server, the first sign-in works without an invitation for the bootstrap owner's email, which the server's operator sets. Only while no user exists yet. After that, everyone else needs an invitation.

## Sign in to the web app

1. Open the Goal web app and enter your work email on the sign-in page.
2. Choose **Email me a sign-in code**. Goal sends a code to that address.
3. On the **Check your email** page, enter the code.

The sign-in page also shows a **Continue with GitHub** button. It is disabled: GitHub sign-in is not built yet.

If the invitation came from a link, open the link first. The invitation page lets you accept it to join, or decline it. If the invitation is for another email address, sign in with that address instead.

## Sign in to the CLI

Run `goal login` in a terminal:

```sh
goal login
```

The CLI shows a code and asks you to open Goal's device page in your browser. On that page, check that the code matches the one in your terminal, then approve it. The browser page says when the CLI is signed in.

Your session goes to the operating system's keychain by default. To keep it in a file beside the CLI's config file instead, pass `--store file` to `goal login`. The file is readable only by you.

To use a personal token or an API key in place of a browser sign-in, pass it on stdin:

```sh
goal login --token < token-file
```

Scripts and CI use an API key from the `GOAL_API_KEY` variable. Keys are created with `goal key create`, described in the [CLI reference](/docs/reference/).

## Check who you are

```sh
goal whoami
```

The output names the account the CLI acts as and the organizations it can reach. If it shows no organization, go to [Organizations](/docs/getting-started/organizations/).

## End a session

```sh
goal logout
```

This ends the CLI's session and forgets it on this machine. Passing `--all` to `goal logout` ends every session of yours, on every machine.

## When sign-in fails

- **The code does not arrive, or sign-in says the email is not invited.** Ask an owner to invite the address, or check that you typed the address the invitation went to.
- **The device page says the CLI was not signed in.** Run `goal login` again and approve the new code.

Goal records each refused sign-up and each new session in its security log, which platform admins can read with `goal admin events`.

## Related

- [Organizations](/docs/getting-started/organizations/): what an organization is and how the CLI picks one.
- [Reference](/docs/reference/): every command and flag.

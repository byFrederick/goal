---

title: Sign in
description: How to sign in to the Goal web app and the goal CLI, and what to do when sign-in fails.
sidebar:
  order: 2
---

You sign in to the web app with a code sent to your work email, or with Google when the server turns it on. The CLI signs in through the web app: it shows a code, and you approve that code in your browser. A new person gets in one of two ways: an invitation, or a verified email at an organization's sign-up domain.

## Before you start

Either of these lets your first sign-in work:

- **An invitation.** Ask an owner of your organization to invite you. The invitation goes to your email address and expires after 72 hours. If it expires, ask for a new one.
- **A sign-up domain.** An owner of an organization can turn on **Only emails at a domain** on the organization's Members page. Anyone whose verified email is exactly at that domain can then sign up without an invitation and joins the organization. A subdomain does not match.

On a new Goal server, the first sign-in also works for the bootstrap owner's email, which the server's operator sets. Only while no user exists yet.

Any other email is refused.

## Sign in to the web app

1. Open the Goal web app and enter your work email on the sign-in page.
2. Choose **Email me a sign-in code**. Goal sends a code to that address.
3. On the **Check your email** page, enter the code.

The sign-in page also shows a **Continue with GitHub** button. It is disabled: GitHub sign-in is not built yet.

### Sign in with Google

When the server runs with `GOAL_AUTH_GOOGLE=on`, the sign-in page shows **Continue with Google**. The same rules apply: the Google account's email must be verified and have an invitation, the bootstrap owner's address or a sign-up domain. When the server runs with `GOAL_AUTH_EMAIL=off`, the page has no email form and Google is the only way in.

If the invitation came from a link, open the link first. The invitation page lets you accept it to join, or decline it. If the invitation is for another email address, sign in with that address instead.

## Install the CLI

There is no released binary. From a clone of the repository, install it with Go:

```sh
go install ./cmd/goal
```

The CLI talks to `http://127.0.0.1:8080` unless `GOAL_API_URL` or `api_url` in the CLI's config file says otherwise. Set it before you run `goal login`:

```sh
export GOAL_API_URL=https://goal.example.com
```

## Sign in to the CLI

Run `goal login` in a terminal:

```sh
goal login
```

1. The CLI prints `Open <origin>/device and enter <code>` and tries to open your browser.
2. On the device page, type the code and press **Continue**.
3. The page shows the code to check. Make sure it matches your terminal, then press **Approve**, or **Deny** if you did not run `goal login`.
4. The browser page says when the CLI is signed in.

Approving needs a sign-in from the last 10 minutes. If yours is older, the page asks you to confirm it's you first.

Your session goes to the operating system's keychain by default. To keep it in a file beside the CLI's config file instead, pass `--store file` to `goal login`. The file is readable only by you.

To use a personal token or an API key in place of a browser sign-in, pass it on stdin:

```sh
goal login --token < token-file
```

Scripts and CI use an API key from the `GOAL_API_KEY` variable. Keys are created with `goal key create`, described in the [CLI reference](/docs/reference/cli/). See also [API keys and personal tokens](#api-keys-and-personal-tokens).

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

## API keys and personal tokens

Both are credentials for scripts and tools, in place of a browser sign-in.

- **API keys** belong to the organization. Open the **API keys** page, choose **New key**, and copy the secret: it is shown once. To end a key, choose **Revoke** on its row.
- **Personal tokens** belong to you. Open your account page, choose **New token**, and copy the secret. To end one, choose **Revoke**.

Only a browser session can create a personal token. A CLI session cannot.

## When sign-in fails

- **No code arrives.** The address may not be allowed to sign up. Goal does not say so: it sends no code. Ask an owner to invite the address, or check that you typed the address the invitation went to.
- **A typed code fails with "That code didn’t work".** Check the code, or ask for a new one. A wrong code and an address that may not sign up look the same.
- **Google says "That Google account isn’t invited to Goal."** Only Google sign-in reports this. Ask an owner to invite that address.
- **Codes stop coming, or every code fails.** Goal sends at most 5 codes per address per hour. Ten failed codes in a day lock the address.
- **The device page says the CLI was not signed in.** Run `goal login` again and approve the new code.

Goal records each refused sign-up and each new session in its security log, which platform admins can read with `goal admin events`.

## Related

- [Organizations](/docs/getting-started/organizations/): what an organization is and how the CLI picks one.
- [CLI reference](/docs/reference/cli/): every command and flag.

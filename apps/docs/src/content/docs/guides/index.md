---
title: Guides
description: Step-by-step tasks with Goal, each one a short sequence that ends in a result.
---

Each guide starts with the result you get and lists the steps to reach it. Commands are the `goal` CLI's; the same actions are on the web app's pages, and each guide names them where they exist.

- [Change a factory definition](./change-a-factory-definition/): pull a version into a folder, edit it, validate it and publish it.
- [Add an agent, skill or rule](./add-an-agent-skill-or-rule/): add the files and the entries in `factory.yaml` that make an agent, a skill or a rule take effect.
- [Submit and follow items](./submit-and-follow-items/): start work from the CLI or the web app, and follow it until a person has to act.
- [Review what enters a factory](./intake/): see where items came from, what has not started, which triggers create work, and try a trigger on a past event.
- [Let agents leave follow-ups](./follow-ups/): let an agent note another item or schedule later work, and file or drop what it left.
- [Connect code to a factory](./connect-code/): list the repositories a factory may use, with read or write access, a token for each and the agents that write.
- [Connect GitHub](./connect-github/): add one organization webhook, give agents GitHub tools and add a GitHub Enterprise or self-managed GitLab host.
- [Receive events with organization webhooks](./webhooks/): connect a sender once, open it to factories and read the delivery log.
- [Connect a webhook in the web app](./connect-a-webhook/): create a webhook on the Webhooks page, paste it into the sender, open it to factories and read the log.
- [Ask an agent what to file](./checks/): add a check to a schedule trigger so an agent reads a source and files what is new.
- [Act on an item](./act-on-an-item/): answer a question, approve, deny, send back, merge, retry, move, note, pause, cancel or re-lane an item.
- [Navigate and use shortcuts](./navigate-and-shortcuts/): find your way around the sidebar, the rail, the menus, search and the phone drawer, and see every keyboard shortcut.
- [Read the Board, the item Timeline and Runs](./read-the-board-timeline-and-runs/): find what is moving, what is waiting and what each agent run did.
- [Read Spend and box usage](./read-spend-and-box-usage/): see what agents cost over a window, and the CPU and memory their run boxes use by agent and station.
- [Self-host with Docker Compose](./self-host-with-docker-compose/): run Goal on one machine with `goal up`.

For what the terms mean, read the [concepts](/docs/concepts/). For every command, flag and endpoint, read the [reference](/docs/reference/).

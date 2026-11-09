---
title: Use the Floor and Home
description: See one factory's line, what waits on you and what happened, on the Floor, and every factory at a glance on Home.
---

You end knowing what needs you now and how each factory is doing. The Floor shows one factory. Home shows all of them.

## The Floor

Open a factory's Floor from the sidebar. It shows these, top down:

- **The line, now:** the stations in order with their counts, then the line's figures: **In progress**, ready items, where new work enters, the expedite lane's use and whether the **Andon** is pulled or clear.
- **Needs you:** what waits on a person in this factory, with a count. An approval has **Deny** and **Approve** on its row. Deny asks for a reason first. On a phone the row has **Deny** and **Review** instead, and Approve is on the item. Other rows have **Decide**, which opens the item. **Open all** (**See all** on a phone) opens the organization's Needs you page. With nothing waiting, it says "Nothing waits on you."
- **Finish setting up:** a checklist for a new factory, with how many rows are done. It shows until the factory's first item is done, and **Hide setup** hides it. A card beside it lists the ways work gets in.
- **Activity:** the factory's latest events, marked **Live**, with new ones at the top.

The Floor also shows **Where work waits** and **Throughput by week**. Throughput says "merged" on a line with repositories and "done" on one without.

## Home

Open Home at `/o/<org>`. It lists every factory in the organization, one row each, with three figures:

- **Needs you:** how many entries wait on a person.
- **Throughput**: items finished a day, with "of N a day" when the factory has a target. While the factory's andon is pulled, this figure becomes **Sent back**, the highest share of items sent back at one station.
- **Spend today:** today's spend, and its share of the day's budget when one is set.

**Needs you first** lists what waits on a person across all factories. A tool call is decided in place: **Approve** allows that call only, and **Deny** refuses it without a note. **Open all** opens the Needs you page. For acting on items, read [Act on an item](/docs/guides/act-on-an-item/).

**Spend this week** shows the week's spend against the factories' budgets. **Archived factories**, under the list, shows archived factories and lets you restore them.

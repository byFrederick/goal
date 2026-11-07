---
title: Read Spend and box usage
description: See what your agents cost, where it went and how much CPU and memory their run boxes use, on the Spend and billing page.
---

You end knowing what your agents cost over a window, which factories and agents spent it, and whether each run box is the right size. Open Spend and billing at `/o/<org>/spend`.

## What Spend shows

Pick a window at the top: **This week**, **This month** or **Last 90 days**. The window is in the URL, so you can share it. Every figure on the page follows it, except the budgets, which always use the last 7 days. With **This week** selected, the first figure is the spend since Monday (UTC).

- **Spend:** the total for the window, this month against last month, the cost per merged item and the share spent on rework.
- **Spend by day:** each day's cost, split by factory.
- **By agent** and **By model:** where the money went.
- **Budgets:** each factory's weekly budget against its last 7 days of spend, and today's spend against a day's share of it (the weekly budget ÷ 7, shown as "about $357 a day"). The budget is set in the factory definition, so this page shows it and does not edit it. At 100% the factory drains: running items finish, and nothing new starts until the last 7 days' spend is under the budget. Today's spend counts every run of the factory, Jonah's included, so it matches **Spend today** on Home.
- **Payers:** which connections pay for the models.

**Export CSV** downloads the window's cost by factory, agent and model. It does not include box usage.

## Box usage

The **Box usage** section shows the CPU and peak memory that runs used, so you can tell which agents and stations use what you pay for, and whether their box size fits.

Switch between **Agents** and **Stations** above the table. Each row is one agent or station in one box size, with these columns:

| Column | Meaning |
| --- | --- |
| Name | The agent or station. With more than one factory, its factory shows under it. |
| Box | The box size the runs used: Small, Medium or Large. **Unknown** for runs from before Goal recorded box sizes. |
| Runs | How many runs reported a CPU time or a peak memory. |
| CPU median, CPU P90 | CPU time per run, from prepare to the run's end. |
| Memory median, Memory P90 | The most memory a run held at once. Goal reads the peak of the box's own cgroup where the box has one, and otherwise the machine's memory in use during the run, which includes the box's own system processes and can read a little higher. |

Rows are sorted by CPU P90, largest first. On a phone each row stacks, with its column names beside the figures.

### Median and P90

The **median** is the middle run: half the runs used less and half used more. **P90** is the figure that 9 in 10 runs stay under, so it shows the heavy runs that a median hides. Compare a box's size with P90 memory, not the median.

A dash means none of the group's runs reported that figure.

### Why some runs are missing

A run counts only when its box measured it. Goal leaves out runs the box couldn't measure, such as runs from before Goal recorded CPU and memory. A row therefore counts fewer runs than the Runs page may list. A run counts in the window in which it started.

When the window has no measured runs, the section says so instead of showing a table.

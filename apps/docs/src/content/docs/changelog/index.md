---
title: Changelog
description: What changed in Goal, week by week.
---

This page lists the changes a person using Goal would notice, newest first, grouped by ISO week. Each entry links its pull request on [github.com/byFrederick/goal](https://github.com/byFrederick/goal). Refactors, CI and tooling changes are left out. The list starts with the changes merged to main from 2026-09-17; the repository's history begins on 2026-10-23.

## Week of 2026-10-26

- The factory builder has New agent, roles, quick fixes and model presets, and drafts can be requested through the API ([#328](https://github.com/byFrederick/goal/pull/328)).
- Jonah has an overview of its proposals, and bake-offs compare models for one agent and apply the winner ([#303](https://github.com/byFrederick/goal/pull/303), [#298](https://github.com/byFrederick/goal/pull/298)).
- Judges score finished runs, and eval cases run before a publish with goal judge and goal eval ([#251](https://github.com/byFrederick/goal/pull/251), [#182](https://github.com/byFrederick/goal/pull/182), [#181](https://github.com/byFrederick/goal/pull/181)).
- Any open item can be cancelled from its page or with goal cancel, and its pull request stays open ([#304](https://github.com/byFrederick/goal/pull/304)).
- The item Timeline shows each run's events as grouped steps in plain words ([#300](https://github.com/byFrederick/goal/pull/300)).
- Sign-up leads to a first factory through onboarding, and anyone signed in can create an organization ([#297](https://github.com/byFrederick/goal/pull/297), [#233](https://github.com/byFrederick/goal/pull/233), [#138](https://github.com/byFrederick/goal/pull/138)).
- Intake can try a trigger on a past event, with the model check ([#294](https://github.com/byFrederick/goal/pull/294), [#232](https://github.com/byFrederick/goal/pull/232)).
- Items and the Board have saved views, Set lane and CSV export ([#287](https://github.com/byFrederick/goal/pull/287), [#237](https://github.com/byFrederick/goal/pull/237), [#236](https://github.com/byFrederick/goal/pull/236)).
- Approvals can be remembered: Always allow lets an agent role call a connection tool without asking again ([#283](https://github.com/byFrederick/goal/pull/283), [#219](https://github.com/byFrederick/goal/pull/219)).
- Agents can ask questions, and the run waits until a person answers from the item page or with goal answer ([#245](https://github.com/byFrederick/goal/pull/245), [#241](https://github.com/byFrederick/goal/pull/241), [#240](https://github.com/byFrederick/goal/pull/240), [#221](https://github.com/byFrederick/goal/pull/221)).
- A gate whose merge is not possible yet waits instead of blocking the item ([#277](https://github.com/byFrederick/goal/pull/277)).
- Knowledge shows which runs used each document, and people can mark a use helped or misled ([#261](https://github.com/byFrederick/goal/pull/261), [#186](https://github.com/byFrederick/goal/pull/186), [#171](https://github.com/byFrederick/goal/pull/171)).
- Search from the sidebar, with Command K, finds items, agents, connections, factories and pages, and goal search does the same from the CLI ([#260](https://github.com/byFrederick/goal/pull/260), [#207](https://github.com/byFrederick/goal/pull/207), [#157](https://github.com/byFrederick/goal/pull/157), [#152](https://github.com/byFrederick/goal/pull/152), [#170](https://github.com/byFrederick/goal/pull/170)).
- Assemblies integrate their parts on one branch and land it at the landing station ([#242](https://github.com/byFrederick/goal/pull/242), [#235](https://github.com/byFrederick/goal/pull/235), [#229](https://github.com/byFrederick/goal/pull/229)).
- The Models and policies screen sets the models an organization allows, its denied commands and its box time limit, with goal policy ([#234](https://github.com/byFrederick/goal/pull/234), [#180](https://github.com/byFrederick/goal/pull/180), [#179](https://github.com/byFrederick/goal/pull/179)).
- The Spend screen and goal spend show what agent runs cost, per item and per factory budget ([#216](https://github.com/byFrederick/goal/pull/216), [#173](https://github.com/byFrederick/goal/pull/173), [#155](https://github.com/byFrederick/goal/pull/155)).
- The Credentials screen creates and imports credentials, and a connection can refer to one as {{name}} ([#209](https://github.com/byFrederick/goal/pull/209), [#239](https://github.com/byFrederick/goal/pull/239), [#184](https://github.com/byFrederick/goal/pull/184), [#148](https://github.com/byFrederick/goal/pull/148)).
- The account page at /account has your profile, appearance, personal tokens and sessions ([#215](https://github.com/byFrederick/goal/pull/215)).
- The Webhooks screen and goal webhooks manage endpoints, their signing secrets and their deliveries ([#224](https://github.com/byFrederick/goal/pull/224), [#225](https://github.com/byFrederick/goal/pull/225)).
- The Intake screen and board triggers create items from events and schedules ([#226](https://github.com/byFrederick/goal/pull/226), [#227](https://github.com/byFrederick/goal/pull/227), [#185](https://github.com/byFrederick/goal/pull/185)).
- The Runs page and goal runs list agent runs across every factory ([#196](https://github.com/byFrederick/goal/pull/196), [#178](https://github.com/byFrederick/goal/pull/178)).
- The Needs you page at /o/<org>/needs-you lists what waits on a person ([#198](https://github.com/byFrederick/goal/pull/198), [#211](https://github.com/byFrederick/goal/pull/211)).
- The Agents page and each agent's page are restyled to the design, and the agent page shows its settings ([#199](https://github.com/byFrederick/goal/pull/199), [#200](https://github.com/byFrederick/goal/pull/200)).
- Factory rules are guidance a factory's agents always follow, listed in the builder and shown on each run's trace ([#84](https://github.com/byFrederick/goal/pull/84), [#90](https://github.com/byFrederick/goal/pull/90), [#93](https://github.com/byFrederick/goal/pull/93), [#95](https://github.com/byFrederick/goal/pull/95)).
- The factory Settings page pauses and resumes a factory, and archives, restores and deletes it ([#74](https://github.com/byFrederick/goal/pull/74), [#75](https://github.com/byFrederick/goal/pull/75), [#76](https://github.com/byFrederick/goal/pull/76)).
- Assemblies can be paused, cancelled and given a part from their page ([#59](https://github.com/byFrederick/goal/pull/59), [#62](https://github.com/byFrederick/goal/pull/62), [#64](https://github.com/byFrederick/goal/pull/64)).
- The Board is restyled to the canvas design, with a lane per agent role, filters, and waiting cards that say what they wait on ([#192](https://github.com/byFrederick/goal/pull/192), [#27](https://github.com/byFrederick/goal/pull/27), [#49](https://github.com/byFrederick/goal/pull/49), [#54](https://github.com/byFrederick/goal/pull/54)).
- Items that touch the same files wait for each other instead of colliding ([#26](https://github.com/byFrederick/goal/pull/26), [#104](https://github.com/byFrederick/goal/pull/104), [#117](https://github.com/byFrederick/goal/pull/117)).
- Before a merge, the engine updates the pull request's branch, and a conflict sends the item back to build ([#57](https://github.com/byFrederick/goal/pull/57), [#60](https://github.com/byFrederick/goal/pull/60)).
- Runs record the plan's base commit and tell later runs what changed on main since ([#126](https://github.com/byFrederick/goal/pull/126)).
- Organization tool defaults apply to every agent at the start of its run ([#188](https://github.com/byFrederick/goal/pull/188)).
- The phone app is installable, with a service worker that caches the app shell ([#61](https://github.com/byFrederick/goal/pull/61), [#63](https://github.com/byFrederick/goal/pull/63)).

## Week of 2026-10-19

- A run whose sandbox is lost is retried, up to a factory setting ([#24](https://github.com/byFrederick/goal/pull/24), [#43](https://github.com/byFrederick/goal/pull/43)).
- Items can name the items they need, and a claim waits until those are done ([#23](https://github.com/byFrederick/goal/pull/23)).
- An expedite item shows as a tinted card in its own station ([#13](https://github.com/byFrederick/goal/pull/13)).
- A gate station whose action is merge merges the pull request when it is approved ([#7](https://github.com/byFrederick/goal/pull/7), [#18](https://github.com/byFrederick/goal/pull/18)).
- Goal waits for GitHub to finish computing a pull request's mergeability before it refuses a merge ([#10](https://github.com/byFrederick/goal/pull/10)).
- The reconnecting banner waits a moment before it shows, and reconnects at once when the page is back ([#11](https://github.com/byFrederick/goal/pull/11)).
- Dark mode, with a System, Light and Dark switch ([#12](https://github.com/byFrederick/goal/pull/12)).
- A factory can set how the line brings assembly branches up to date and what happens on a conflict ([#20](https://github.com/byFrederick/goal/pull/20)).
- goal version prints the module version and build information ([#1](https://github.com/byFrederick/goal/pull/1)).

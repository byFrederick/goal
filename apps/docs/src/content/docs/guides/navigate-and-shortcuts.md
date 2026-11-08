---
title: Navigate and use shortcuts
description: Move around the web app with the sidebar, the rail, the organization and account menus, search and the phone drawer, and see every keyboard shortcut.
---

You know where each part of the web app is, and you can reach any page from the sidebar, search or a shortcut. The sidebar holds the organization's pages and factories. Press `?` at any time to see the shortcuts list.

## The sidebar

The sidebar sits on the left of every organization page, from 768 px wide up.

- **Organization menu.** Choose the organization's name at the top. The menu switches organizations, creates one (anyone signed in, and everyone when sign-in is off), and opens the organization's pages: Members and roles, Spend and billing, Models and policies, API keys (where you may read them) and Organization settings.
- **Search.** Choose the search icon, or press `⌘K` (`Ctrl K` on Windows and Linux). See [Search](#search).
- **Collapse.** Choose the collapse icon, or drag the sidebar's right edge narrower than about 160 px, to fold it into the rail. Drag it back wider, or choose the expand icon, to open it again. Double-click the edge to reset its width.
- **New item.** Choose New item, or press `C`, to open the composer. See [Submit and follow items](/docs/guides/submit-and-follow-items/).
- **Organization pages.** Home, Needs you (with the number of items waiting on you), Runs, Access, Secrets, Webhooks and Knowledge. Pages you may not open are not listed.
- **Factories.** Each factory opens to its pages: Floor, Board, Items, Agents, Intake, Jonah, Definition and Settings. A factory's count shows the items waiting on you. A red dot means the factory has an andon pull or blocked items.
- **Spend this week.** A row above the account shows this week's spend, with a bar against the budget when one is set. It opens Spend and billing.
- **Account.** The person at the bottom of the sidebar, marked with an up and down chevron, opens the account menu.

## The rail

When the sidebar is folded, it becomes a narrow rail of icons. The rail has the same pages and factories, shown as icons with their counts. Hover or focus an icon to see its name. The `+` icon opens the composer. The rail's avatar opens the account menu.

## The account menu

Choose the avatar and name at the bottom of the sidebar, or the avatar in the rail. The menu shows your name and your role in the organization, then these rows:

- **Theme.** Choose Auto, Light or Dark.
- **Profile** and **Personal tokens.** Open your account page.
- **Notifications.** Opens the notifications section of your account page.
- **Connected apps.** Shows "Not available yet" for now.
- **Keyboard shortcuts.** Opens the shortcuts list.
- **Help and docs.** Opens this documentation in the same tab.
- **Sign out** and **Sign out everywhere.**

## Search

Press `⌘K` (or `Ctrl K`), or choose the search icon. Type to search the organization. Results are grouped:

- **Items** match by title or by exact number.
- **Pages** match by name: Home, Members and roles, API keys, Access, Secrets, Knowledge and Settings, where you may open them. When a factory is open, its Floor, Board, Items, Definition, Settings and Jonah are listed too.
- **Agents** and **Secrets** match by name.
- **Factories** match by name.
- **Actions** offer New item about your query, Pause or Resume the line of the open factory, and Approve for each item waiting on you.

Use `↑` and `↓` to move between results, `Enter` to open the one you chose, and `Esc` to close search. The results update as you type and stay on screen while the next search loads.

## The account page

The account page, titled Your account, holds your own settings. Open it from the account menu with Profile. Its left list jumps to each section:

- **Profile.** Name is the name others see. Email for notifications is the address Goal uses for notification email and Slack; blank uses your sign-in email. Change either, then choose Save changes in the top bar.
- **Appearance.** Choose Match device, Light or Dark. The account menu's Theme row sets the same choice, where Match device is called Auto.
- **Sessions.** Shows This browser, marked This device. Sign out everywhere ends your sign-in on every device.

The Notifications section is covered in [Set up notifications](/docs/guides/set-up-notifications/).

## The update toast

When a new version of Goal is available, the toast "A new version of Goal is ready" appears and stays until you act. Choose Reload to switch to the new version now. Until you do, the page keeps running the version it loaded.

## The top bar

From 768 px, the top bar's breadcrumb starts with the organization (or the factory on a factory page) and links back to it.

## The phone

Below 768 px, the sidebar is hidden. The top bar has the Goal mark, which opens the drawer, then the page title, then the page's main action. Other actions sit behind More actions. The drawer holds the same content as the sidebar: the organization menu, search, New item, the organization's pages, the factories and the account. The tab bar at the bottom opens Needs you, Floor, Board, Items and More. More opens the drawer. The current tab shows its label in ink with a thin ink line along its top edge.

## Shortcuts

These are the shortcuts the web app binds. A shortcut does not work while you type in a field, except `⌘K`, which works from fields but not in the definition's code editor. A shortcut with a focus condition says so in the table.

| Shortcut | What it does |
| --- | --- |
| `⌘K` or `Ctrl K` | Open search |
| `C` | Open the New item composer |
| `?` | Open the keyboard shortcuts list |
| `J` and `K` | Move to the next and previous row on Needs you |
| `A` | Approve the selected item on Needs you |
| `D` | Deny the selected item on Needs you |
| `M` | Open the Move menu, with a Board card focused |
| `Space` | Pick up the focused Board card. While carrying it, `Space` or `Enter` drops it |
| `←` and `→` | Choose a station while carrying a Board card |
| `Esc` | Cancel a Board card's move |
| `←` and `→` | On the focused sidebar resize handle: step the width by 16 px (`→` also opens the rail) |
| `Home` and `End` | On the sidebar resize handle: fold the sidebar, or set it to its widest, 400 px |
| `Enter` | On the sidebar resize handle: reset the width to 256 px |
| `⌘Z` or `Ctrl Z` | Undo an edit in the definition editor, outside its text fields |
| `⇧⌘Z` or `Ctrl Shift Z` | Redo an edit in the definition editor |

`?` opens the list from anywhere, except from inside a text field or another dialog or menu. The list opens at once, without animation. From the account menu, Keyboard shortcuts opens the same list with the usual dialog motion. `Esc` closes it.

For what the terms on these pages mean, read the [concepts](/docs/concepts/). For the screens each page holds, read the [guides](/docs/guides/).

// Members and roles, Invite-Accept and Transfer ownership (spec 3 §10.2, PR 3-11) at every width
// against the mock with sign-in on: an owner's page with the reasons a role cannot change, factory
// roles applied at once, inviting, resending and cancelling, removing; a member's read-only page and
// leaving; joining through an invitation and its other states; handing the organization to an admin.
import { type APIRequestContext, expect, type Page, test } from '@playwright/test';
import {
  expectAccessible,
  expectNoHorizontalScroll,
  expectToast,
  isPhone,
  openNavigation,
  orgMarkOf,
  orgNav,
  recordToasts,
} from './support';

/** The mock's invitations (mock/people.ts). */
const invitation = {
  pending: '/invite/019a1c00-0000-7000-8000-0000000e0001',
  cancelled: '/invite/019a1c00-0000-7000-8000-0000000e0002',
  toAMember: '/invite/019a1c00-0000-7000-8000-0000000e0003',
};

/** Turns sign-in on with `email` signed in: the cookie lands in the page's browser. */
async function signedInAs(request: APIRequestContext, email = 'f.cruz@example.com', fresh = true) {
  const res = await request.post('/__mock/reset', {
    data: { auth: { mode: 'session', signed_in: fresh ? 'fresh' : 'stale', email } },
  });
  expect(res.ok()).toBe(true);
}

test.beforeEach(async ({ page }) => {
  await recordToasts(page);
});

// The files after this one run with sign-in off, as the mock's seed has it.
test.afterEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

const heading = (page: Page) => page.getByRole('heading', { level: 1, name: /^Members/ });
const people = (page: Page) => page.getByRole('region', { name: /members/ });

/**
 * Whether the page lists people as cards: below 720 px of main (phones, and tablets beside the
 * sidebar); a table wider.
 */
const listsCards = (page: Page) => (page.viewportSize()?.width ?? 0) < 1024;

/** A person's entry: their table row, or their card. */
const personOf = (page: Page, name: string) =>
  listsCards(page)
    ? people(page).getByRole('button', { name: new RegExp(`^${name}`) })
    : people(page).getByRole('row', { name: new RegExp(name) });

/** Opens a person's details: their sheet, from the card on a phone or the row's More menu wider. */
async function openPerson(page: Page, name: string) {
  if (listsCards(page)) {
    await personOf(page, name).click();
    return page.getByRole('dialog', { name });
  }
  await page.getByRole('button', { name: `More for ${name}` }).click();
  await page.getByRole('menuitem', { name: 'Factory roles…' }).click();
  return page.getByRole('dialog', { name });
}

test('an owner sees everyone, why a role cannot change, and changes factory roles at once', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/acme/members');
  await expect(heading(page)).toBeVisible();
  await expect(page).toHaveTitle('Members and roles · Acme · Goal');
  await expect(page.getByText('4 members, 2 invites waiting')).toBeVisible();
  await expect(page.getByText('3 factories. You’re an owner.')).toBeVisible({ visible: !isPhone(page) });
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  if (!listsCards(page)) {
    // The only owner's role cannot change, and the page says why; an admin's can.
    const own = page.getByRole('combobox', { name: 'Role of F. Cruz in Acme' });
    await expect(own).toBeDisabled();
    await expect(own).toHaveAccessibleDescription('Acme needs at least one owner');
    await expect(page.getByRole('combobox', { name: 'Role of M. Okafor in Acme' })).toBeEnabled();
    await expect(personOf(page, 'J. Lindqvist')).toContainText('goal approver, sre editor');
  }
  await expect(page.getByRole('region', { name: /Waiting to join/ })).toContainText('d.ross@example.com');

  // Lindqvist becomes a viewer of goal: the sheet changes at once and the list follows.
  const details = await openPerson(page, 'J. Lindqvist');
  const goal = details.getByRole('group', { name: 'goal: role of J. Lindqvist' });
  await goal.getByText('Viewer').click();
  await expect(goal.getByRole('radio', { name: 'Viewer' })).toBeChecked();
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(personOf(page, 'J. Lindqvist')).toContainText('goal viewer, sre editor');

  // An owner's factory roles are the organization's: every factory, Editor, not changeable.
  const okafor = await openPerson(page, 'M. Okafor');
  const sre = okafor.getByRole('group', { name: 'sre: role of M. Okafor' });
  await expect(sre.getByRole('radio', { name: /^Editor/ })).toBeChecked();
  await expect(sre.getByRole('radio', { name: /^None/ })).toBeDisabled();
});

test('an owner invites people with factory roles, resends and cancels an invitation', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/acme/members');
  await page.getByRole('button', { name: /^Invite/ }).click();
  const dialog = page.getByRole('dialog', { name: 'Invite people to Acme' });
  await expect(dialog.getByLabel('Email addresses')).toBeFocused();
  await expectAccessible(page);

  // Something that is not an address is said at the field.
  await dialog.getByLabel('Email addresses').fill('k.ito@example.com\nnobody');
  await dialog.getByRole('button', { name: 'Send invitations' }).click();
  await expect(dialog.getByLabel('Email addresses')).toHaveAccessibleDescription(/^Not an email address: nobody\./);

  // An existing member is refused by name; the other address goes, with its factory role.
  await dialog.getByLabel('Email addresses').fill('k.ito@example.com\na.ortiz@example.com');
  await dialog.getByLabel('sre').selectOption('approver');
  await dialog.getByRole('button', { name: 'Send invitations' }).click();
  await expect(dialog.getByText('Not sent to a.ortiz@example.com: Already a member of Acme.')).toBeVisible();
  await expect(dialog.getByLabel('Email addresses')).toHaveValue('a.ortiz@example.com');
  await expectToast(page, '1 invitation sent');
  await page.keyboard.press('Escape');
  await expect(page.getByText('4 members, 3 invites waiting')).toBeVisible();

  // Admins and owners hold every factory role: inviting one asks for none.
  await page.getByRole('button', { name: /^Invite/ }).click();
  await dialog.getByRole('radio', { name: /^Admin/ }).check();
  await expect(dialog.getByRole('group', { name: 'Factory roles' })).toHaveCount(0);
  await page.keyboard.press('Escape');

  await page.getByRole('button', { name: 'Resend the invitation to k.ito@example.com' }).click();
  await expectToast(page, 'Sent the invitation to k.ito@example.com again');
  await page.getByRole('button', { name: 'Revoke the invitation to k.ito@example.com' }).click();
  await expectToast(page, 'Cancelled the invitation to k.ito@example.com');
  await expect(page.getByText('4 members, 2 invites waiting')).toBeVisible();
});

test('an owner turns sign-up by domain on and off; a member sees the switch disabled', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/acme/members');
  const domain = page.getByRole('switch', { name: 'Restrict sign-up to a domain' });
  await expect(domain).toHaveAttribute('aria-checked', 'false');
  await expect(page.getByText('Anyone at example.com joins as a Viewer')).toBeVisible();

  await domain.click();
  await expect(domain).toHaveAttribute('aria-checked', 'true');
  await expect(page.getByText('Anyone at example.com joins as a Viewer')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  await domain.click();
  await expect(domain).toHaveAttribute('aria-checked', 'false');
  // The join link is not in the API: its controls stay unavailable.
  await expect(page.getByRole('button', { name: 'Copy' })).toHaveAttribute('aria-disabled', 'true');
});

test('a member sees sign-up by domain disabled and nothing else changes', async ({ page }) => {
  await signedInAs(page.request, 'a.ortiz@example.com');
  await page.goto('/o/acme/members');
  const domain = page.getByRole('switch', { name: 'Restrict sign-up to a domain' });
  await expect(domain).toHaveAttribute('aria-disabled', 'true');
  await expect(domain).toHaveAttribute('aria-checked', 'false');
  await expect(page.getByRole('region', { name: 'Invite link' })).toHaveCount(0);
});

test('an owner makes someone an admin and removes someone, after confirming', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/acme/members');
  if (listsCards(page)) {
    const sheet = await openPerson(page, 'A. Ortiz');
    await sheet.getByLabel('Role of A. Ortiz in Acme').selectOption('admin');
  } else {
    await page.getByRole('combobox', { name: 'Role of A. Ortiz in Acme' }).selectOption('admin');
  }
  await expectToast(page, 'A. Ortiz is now an admin');
  if (listsCards(page)) {
    await page.getByRole('dialog', { name: 'A. Ortiz' }).getByRole('button', { name: 'Remove from Acme…' }).click();
  } else {
    await page.getByRole('button', { name: 'More for A. Ortiz' }).click();
    await page.getByRole('menuitem', { name: 'Remove from Acme…' }).click();
  }
  const confirm = page.getByRole('dialog', { name: 'Remove A. Ortiz from Acme?' });
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await expectAccessible(page);
  await confirm.getByRole('button', { name: 'Remove' }).click();
  await expectToast(page, 'Removed A. Ortiz from Acme');
  await expect(page.getByText('3 members, 2 invites waiting')).toBeVisible();
});

test('a change with an old sign-in asks to confirm it’s you, then goes through', async ({ page }) => {
  await signedInAs(page.request, 'f.cruz@example.com', false);
  await page.goto('/o/acme/members');
  const details = await openPerson(page, 'A. Ortiz');
  await details.getByRole('group', { name: 'sre: role of A. Ortiz' }).getByText('Viewer').click();
  const reauth = page.getByRole('dialog', { name: 'Confirm it’s you' });
  await reauth.getByRole('button', { name: 'Email me a code' }).click();
  await reauth.getByLabel('Code').fill('000000');
  await expect(reauth).toBeHidden();
  await page.keyboard.press('Escape');
  await expect(personOf(page, 'A. Ortiz')).toContainText('sre viewer, support viewer');
});

test('a member sees everyone read-only, and leaves', async ({ page }) => {
  await signedInAs(page.request, 'a.ortiz@example.com');
  await page.goto('/o/acme/members');
  await expect(page.getByText('View only. Owners and admins of Acme invite people and change roles.')).toBeVisible();
  await expect(page.getByRole('button', { name: /^Invite/ })).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: /^Role of/ })).toHaveCount(0);
  await expect(page.getByRole('region', { name: /Waiting to join/ })).toHaveCount(0);
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  if (listsCards(page)) {
    await personOf(page, 'A. Ortiz').click();
    await page.getByRole('dialog', { name: 'A. Ortiz' }).getByRole('button', { name: 'Leave Acme…' }).click();
  } else {
    await personOf(page, 'A. Ortiz').getByRole('button', { name: 'Leave Acme…' }).click();
  }
  const confirm = page.getByRole('dialog', { name: 'Leave Acme?' });
  await confirm.getByRole('button', { name: 'Leave Acme' }).click();
  await expectToast(page, 'You left Acme');
  await expect(page).toHaveURL(/\/o\/goal$/);
});

test('an invitee signs in from the link, sees the invitation and joins', async ({ page, request }) => {
  await request.post('/__mock/reset', { data: { auth: { mode: 'session' } } });
  await page.goto(invitation.pending);
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to see your invitation' })).toBeVisible();
  await page.getByRole('link', { name: 'Sign in' }).click();
  await page.getByLabel('Email').fill('d.ross@example.com');
  await page.getByRole('button', { name: 'Email me a code' }).click();
  await page.getByLabel('Code').fill('000000');

  await expect(
    page.getByRole('heading', { level: 1, name: 'M. Okafor invited you to join Acme as a member.' }),
  ).toBeVisible();
  await expect(page.getByText('sre, editor')).toBeVisible();
  await expect(page.getByText('d.ross@example.com')).toBeVisible();
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);
  await page.getByRole('button', { name: 'Join Acme' }).click();
  await expect(page).toHaveURL(/\/o\/acme$/);
  await expectToast(page, 'You joined Acme');

  // Signed up by code, they have no name yet (Better Auth leaves it empty): Goal names them by
  // their email, with its initials, until they choose one.
  await orgNav(page).filter({ visible: true }).getByRole('button', { name: 'DR, d.ross@example.com, account' }).click();
  const account = page.getByRole('dialog', { name: 'Account' });
  await expect(account.getByText('d.ross@example.com', { exact: true })).toBeVisible();
  await expect(account.getByText('DR', { exact: true })).toBeVisible();
  await expect(account.getByText('?', { exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');

  // Joined as a member with the invitation's factory role.
  await page.goto('/o/acme/members');
  await expect(personOf(page, 'd.ross')).toContainText('sre editor');
  await expect(personOf(page, 'd.ross')).toContainText('DR');
});

test('an invitation for someone else, one that cannot be used, and one to a member', async ({ page }) => {
  await signedInAs(page.request, 'j.doe@example.com');
  await page.goto(invitation.pending);
  await expect(
    page.getByRole('heading', { level: 1, name: 'This invitation is for another email address' }),
  ).toBeVisible();
  await expect(page.getByText('You’re signed in as j.doe@example.com.')).toBeVisible();
  await expectAccessible(page);

  await page.goto(invitation.cancelled);
  await expect(page.getByRole('heading', { level: 1, name: 'This invitation can’t be used' })).toBeVisible();
  await expect(page.getByText('It may have expired or been cancelled. Ask for a new one.')).toBeVisible();

  await signedInAs(page.request, 'a.ortiz@example.com');
  await page.goto(invitation.toAMember);
  await expect(page.getByRole('heading', { level: 1, name: 'You’re already in Goal' })).toBeVisible();
  await page.getByRole('link', { name: 'Open Goal' }).click();
  await expect(page).toHaveURL(/\/o\/goal$/);

  // Declining keeps the person out and says so.
  await signedInAs(page.request, 'd.ross@example.com');
  await page.goto(invitation.pending);
  await page.getByRole('button', { name: 'Decline' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Invitation declined' })).toBeVisible();
});

test('an owner hands the organization to an admin and becomes one', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/goal/settings');
  // Goal has no admin: there is nobody to hand it to.
  await page.getByRole('button', { name: 'Transfer ownership…' }).click();
  const none = page.getByRole('dialog', { name: 'Transfer ownership of Goal' });
  await expect(none.getByText('Only an admin can become the owner, and Goal has none.')).toBeVisible();
  await expect(none.getByRole('link', { name: 'Open Members and roles' })).toBeVisible();
  await expect(none.getByRole('button', { name: 'Transfer ownership' })).toBeDisabled();
  await page.keyboard.press('Escape');

  await page.goto('/o/acme/settings');
  await page.getByRole('button', { name: 'Transfer ownership…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Transfer ownership of Acme' });
  await expect(dialog.getByLabel('New owner')).toHaveValue(/.+/);
  await expect(dialog.getByLabel('New owner').locator('option')).toHaveText(['M. Okafor, admin']);
  await expectAccessible(page);
  await expect(dialog.getByRole('button', { name: 'Transfer ownership' })).toBeDisabled();
  await dialog.getByLabel('Type Acme to confirm').fill('Acme');
  await dialog.getByRole('button', { name: 'Transfer ownership' }).click();
  await expectToast(page, 'You’re now an admin of Acme');
  // An admin keeps the settings but not the Danger zone.
  await expect(page.getByRole('heading', { name: 'Danger zone' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Save changes' })).toBeVisible();
  await expect(page.getByText('3 factories. You’re an admin.')).toBeVisible({ visible: !isPhone(page) });
});

test('a member sees the settings read-only', async ({ page }) => {
  await signedInAs(page.request, 'a.ortiz@example.com');
  await page.goto('/o/acme/settings');
  await expect(page.getByText('View only. Owners and admins of Acme change its settings.')).toBeVisible();
  await expect(page.getByLabel('Name')).toHaveAttribute('readonly', '');
  await expect(page.getByRole('button', { name: 'Save changes' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Danger zone' })).toHaveCount(0);
  await expectAccessible(page);
});

test('the organization menu says the caller’s role and opens Members and roles', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/acme');
  // The same organization menu at every width; on a phone it opens from the drawer's top row.
  await openNavigation(page);
  await orgMarkOf(page).click();
  const menu = page.getByRole('menu');
  await expect(menu.getByRole('menuitem', { name: /^Acme\s?3 factories, owner(\s?Current)?$/ })).toBeVisible();
  await menu.getByRole('menuitem', { name: 'Members and roles' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/members$/);
  await expect(heading(page)).toBeVisible();
});

test('Basics names who approves and links an owner to Members', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/acme/f/sre/definition/basics');
  await expect(
    page.getByText(
      'Owners and admins of Acme, and this factory’s approvers and editors: F. Cruz, J. Lindqvist, M. Okafor.',
    ),
  ).toBeVisible();
  await page.getByRole('link', { name: 'Change in Members' }).click();
  await expect(page).toHaveURL(/\/o\/acme\/members$/);
});

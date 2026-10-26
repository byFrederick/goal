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

/** Opens a person's details: their sheet on a phone, the factory panel from the row's menu wider. */
async function openPerson(page: Page, name: string) {
  if (listsCards(page)) {
    await personOf(page, name).click();
    return page.getByRole('dialog', { name });
  }
  await page.getByRole('button', { name: `Actions for ${name}` }).click();
  await page.getByRole('menuitem', { name: 'Factory roles…' }).click();
  return page.getByRole('region', { name: `${name} in each factory` });
}

test('an owner sees everyone, why a role cannot change, and changes factory roles at once', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/acme/members');
  await expect(heading(page)).toBeVisible();
  await expect(page).toHaveTitle('Members and roles · Acme · Goal');
  await expect(people(page).getByRole('heading', { name: '6 members, 1 invite' })).toBeVisible();
  await expect(page.getByText('3 factories. You’re an owner.')).toBeVisible({ visible: !isPhone(page) });
  await expectAccessible(page);
  await expectNoHorizontalScroll(page);

  if (!listsCards(page)) {
    // The only owner's role cannot change, and the page says why; an admin's can.
    const own = page.getByRole('combobox', { name: 'Role of F. Cruz in Acme' });
    await expect(own).toBeDisabled();
    await expect(own).toHaveAccessibleDescription('Acme needs at least one owner');
    await expect(page.getByRole('combobox', { name: 'Role of M. Okafor in Acme' })).toBeEnabled();
    await expect(personOf(page, 'J. Lindqvist')).toContainText('sre approver, support viewer');
    await expect(personOf(page, 'd.ross@example.com')).toContainText('Invited');
  }

  // Lindqvist becomes an editor of goal: the panel (or sheet) changes at once and the list follows.
  const details = await openPerson(page, 'J. Lindqvist');
  const goal = details.getByRole('group', { name: 'goal: role of J. Lindqvist' });
  await goal.getByText('Editor').click();
  await expect(goal.getByRole('radio', { name: 'Editor' })).toBeChecked();
  await expectAccessible(page);
  if (listsCards(page)) await page.keyboard.press('Escape');
  await expect(personOf(page, 'J. Lindqvist')).toContainText('goal editor, sre approver, support viewer');

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
  await dialog.getByLabel('Email addresses').fill('k.ito@example.com\ns.lee@example.com');
  await dialog.getByLabel('sre').selectOption('approver');
  await dialog.getByRole('button', { name: 'Send invitations' }).click();
  await expect(dialog.getByText('Not sent to s.lee@example.com: Already a member of Acme.')).toBeVisible();
  await expect(dialog.getByLabel('Email addresses')).toHaveValue('s.lee@example.com');
  await expectToast(page, '1 invitation sent');
  await page.keyboard.press('Escape');
  await expect(people(page).getByRole('heading', { name: '6 members, 2 invites' })).toBeVisible();

  // Admins and owners hold every factory role: inviting one asks for none.
  await page.getByRole('button', { name: /^Invite/ }).click();
  await dialog.getByRole('radio', { name: /^Admin/ }).check();
  await expect(dialog.getByRole('group', { name: 'Factory roles' })).toHaveCount(0);
  await page.keyboard.press('Escape');

  await page.getByRole('button', { name: 'Resend the invitation to k.ito@example.com' }).click();
  await expectToast(page, 'Sent the invitation to k.ito@example.com again');
  await page.getByRole('button', { name: 'Cancel the invitation to k.ito@example.com' }).click();
  await expectToast(page, 'Cancelled the invitation to k.ito@example.com');
  await expect(people(page).getByRole('heading', { name: '6 members, 1 invite' })).toBeVisible();
});

test('an owner makes someone an admin and removes someone, after confirming', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/acme/members');
  if (listsCards(page)) {
    const sheet = await openPerson(page, 'S. Lee');
    await sheet.getByLabel('Role of S. Lee in Acme').selectOption('admin');
  } else {
    await page.getByRole('combobox', { name: 'Role of S. Lee in Acme' }).selectOption('admin');
  }
  await expectToast(page, 'S. Lee is now an admin');
  if (listsCards(page)) {
    await page.getByRole('dialog', { name: 'S. Lee' }).getByRole('button', { name: 'Remove from Acme…' }).click();
  } else {
    await page.getByRole('button', { name: 'Actions for S. Lee' }).click();
    await page.getByRole('menuitem', { name: 'Remove from Acme…' }).click();
  }
  const confirm = page.getByRole('dialog', { name: 'Remove S. Lee from Acme?' });
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await expectAccessible(page);
  await confirm.getByRole('button', { name: 'Remove' }).click();
  await expectToast(page, 'Removed S. Lee from Acme');
  await expect(people(page).getByRole('heading', { name: '5 members, 1 invite' })).toBeVisible();
});

test('a change with an old sign-in asks to confirm it’s you, then goes through', async ({ page }) => {
  await signedInAs(page.request, 'f.cruz@example.com', false);
  await page.goto('/o/acme/members');
  const details = await openPerson(page, 'P. Nair');
  await details.getByRole('group', { name: 'goal: role of P. Nair' }).getByText('Viewer').click();
  const reauth = page.getByRole('dialog', { name: 'Confirm it’s you' });
  await reauth.getByRole('button', { name: 'Email me a code' }).click();
  await reauth.getByLabel('Code').fill('000000');
  await expect(reauth).toBeHidden();
  if (listsCards(page)) await page.keyboard.press('Escape');
  await expect(personOf(page, 'P. Nair')).toContainText('goal viewer, support editor');
});

test('a member sees everyone read-only, and leaves', async ({ page }) => {
  await signedInAs(page.request, 'a.ortiz@example.com');
  await page.goto('/o/acme/members');
  await expect(page.getByText('View only. Owners and admins of Acme invite people and change roles.')).toBeVisible();
  await expect(page.getByRole('button', { name: /^Invite/ })).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: /^Role of/ })).toHaveCount(0);
  await expect(personOf(page, 'd.ross@example.com')).toHaveCount(0);
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
  await expect(page).toHaveURL(/\/o\/example-labs$/);
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
  await expect(page.getByText('sre, viewer')).toBeVisible();
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
  await expect(personOf(page, 'd.ross')).toContainText('sre viewer');
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
  await expect(page.getByRole('heading', { level: 1, name: 'You’re already in Example Labs' })).toBeVisible();
  await page.getByRole('link', { name: 'Open Example Labs' }).click();
  await expect(page).toHaveURL(/\/o\/example-labs$/);

  // Declining keeps the person out and says so.
  await signedInAs(page.request, 'd.ross@example.com');
  await page.goto(invitation.pending);
  await page.getByRole('button', { name: 'Decline' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Invitation declined' })).toBeVisible();
});

test('an owner hands the organization to an admin and becomes one', async ({ page }) => {
  await signedInAs(page.request);
  await page.goto('/o/example-labs/settings');
  // Example Labs has no admin: there is nobody to hand it to.
  await page.getByRole('button', { name: 'Transfer ownership…' }).click();
  const none = page.getByRole('dialog', { name: 'Transfer ownership of Example Labs' });
  await expect(none.getByText('Only an admin can become the owner, and Example Labs has none.')).toBeVisible();
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
  await orgMarkOf(page).click();
  if (isPhone(page)) {
    const sheet = page.getByRole('dialog', { name: 'Switch organization' });
    await expect(sheet.getByRole('link', { name: /^Acme\s?3 factories, owner/ })).toBeVisible();
    await sheet.getByRole('link', { name: 'Members and roles' }).click();
  } else {
    const menu = page.getByRole('menu', { name: 'Ac, Acme organization, switch organization' });
    await expect(menu.getByRole('menuitemradio', { name: /^Acme\s?3 factories, owner$/ })).toBeVisible();
    await menu.getByRole('menuitem', { name: 'Members and roles' }).click();
  }
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

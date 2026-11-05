// The item page's question card (ADR 0018, screens/daily-work/item): the agent asks, a person answers
// with one of its choices or in their own words, and the card goes while the timeline shows the answer.
// Runs against the mock API (GOAL_API=mock): item 14 of the software factory holds the pictured question.
import { expect, type Page, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

// The Timeline panel shows the answer in a container 720 px wide or more. Narrower, the item page lists
// "Latest steps" and "Human touches" instead (item-page.tsx), so the recorded answer shows there.
async function expectAnswerShown(page: Page, timelineText: string) {
  const timeline = page.getByRole('tabpanel', { name: 'Timeline' });
  if (await timeline.isVisible()) {
    await expect(timeline.getByText(timelineText).first()).toBeVisible();
  } else {
    await expect(page.getByText('Local user question answered.').locator('visible=true').first()).toBeVisible();
  }
}

test('answering the question with one of its choices removes the card and records the answer', async ({ page }) => {
  await page.goto('/o/acme/f/software/items/14');

  const card = page.getByRole('region', { name: 'frontend-engineer asks you' });
  await expect(card).toBeVisible();
  await expect(
    card.getByText(
      "Should the sidebar show this week's spend for the whole organization, or only for the factory you are in?",
    ),
  ).toBeVisible();

  await card.getByRole('button', { name: 'Only this factory' }).click();

  await expect(card).toBeHidden();
  await expectAnswerShown(page, 'Local user answered');
});

test('answering the question in your own words sends the text and removes the card', async ({ page }) => {
  await page.goto('/o/acme/f/software/items/14');

  const card = page.getByRole('region', { name: 'frontend-engineer asks you' });
  await expect(card).toBeVisible();

  await card.getByRole('button', { name: 'Write an answer' }).click();
  const words = 'Show the spend for this factory, with the weekly budget.';
  await card.getByRole('textbox', { name: 'Your answer' }).fill(words);
  await card.getByRole('button', { name: 'Send' }).click();

  await expect(card).toBeHidden();
  await expectAnswerShown(page, words);
});

// The item page's question card (ADR 0018, screens/daily-work/item): the agent asks, a person answers
// with one of its choices or in their own words, and the card goes while the timeline shows the answer.
// Runs against the mock API (GOAL_API=mock): item 14 of the goal factory holds the pictured question.
import { expect, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  await request.post('/__mock/reset');
});

test('answering the question with one of its choices removes the card and records the answer', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/14');

  const card = page.getByRole('region', { name: 'frontend-engineer asks you' });
  await expect(card).toBeVisible();
  await expect(
    card.getByText(
      "Should the sidebar show this week's spend for the whole organization, or only for the factory you are in?",
    ),
  ).toBeVisible();

  await card.getByRole('button', { name: 'Only this factory' }).click();

  await expect(card).toBeHidden();
  await expect(page.getByText(/answered/)).toBeVisible();
});

test('answering the question in your own words sends the text and removes the card', async ({ page }) => {
  await page.goto('/o/acme/f/goal/items/14');

  const card = page.getByRole('region', { name: 'frontend-engineer asks you' });
  await expect(card).toBeVisible();

  await card.getByRole('button', { name: 'Write an answer' }).click();
  const words = 'Show the spend for this factory, with the weekly budget.';
  await card.getByRole('textbox', { name: 'Your answer' }).fill(words);
  await card.getByRole('button', { name: 'Send' }).click();

  await expect(card).toBeHidden();
  await expect(page.getByText(words)).toBeVisible();
});

// The Board's line says when a used-up weekly budget lets the line resume.
import { expect, test } from '@playwright/test';
import { expectNoClippedElements, expectNoHorizontalScroll } from './support';

test('the line says the weekly budget is used up and when it resumes', async ({ page, request }) => {
  await request.post('/__mock/reset', { data: { usedUpBudget: 'sre' } });
  await page.goto('/o/acme/f/sre/board');
  const note = page.getByText(/Weekly budget used up: .* Resumes about \w{3} \d{2}:\d{2}\./);
  await expect(note).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectNoClippedElements(page);
});

import { expect, it } from 'vitest';
import { intakeOf } from './intake.ts';

const file = `spec:
  board:
    triggers:
      - name: on-already
        on: schedule
        cron: "0 9 * * MON"
        create: { type: review, station: inbox }
      - name: starter
        enabled: false
        on: github.issues.labeled
        create: { type: review, station: inbox }
`;

it('suggests the triggers the definition ships disabled, as the API does', () => {
  const intake = intakeOf('cloud-costs', file);
  expect(intake.triggers.map((t) => t.name)).toEqual(['on-already', 'starter']);
  expect(intake.suggestions.map((s) => s.name)).toEqual(['starter']);
});

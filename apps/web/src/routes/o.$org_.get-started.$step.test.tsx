// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { Route } from './o.$org_.get-started.$step';

type Head = (arg: unknown) => { meta: { title: string }[] };
const head = Route.options.head as unknown as Head;

describe('the onboarding step route head', () => {
  it('names the step and the organization', () => {
    const title = head({ match: { params: { step: 'code' }, context: { org: { name: 'Acme' } } } }).meta[0]?.title;
    expect(title).toBe('Code · Acme · Goal');
  });

  it('does not throw for an unknown step, where beforeLoad threw before setting the organization', () => {
    const title = head({ match: { params: { step: 'not-a-step' }, context: {} } }).meta[0]?.title;
    expect(title).toBe('Goal');
  });
});

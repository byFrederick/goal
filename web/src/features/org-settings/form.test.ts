import { describe, expect, test } from 'vitest';
import type { OrgSettings } from '../../api/types';
import { changedFields, formOf, slotNote, validate } from './form';

const acme: OrgSettings = {
  name: 'Acme',
  slug: 'acme',
  mark: 'Ac',
  slots: {
    max: 6,
    factories: [
      { factory_id: 'f-goal', name: 'goal', max: 3 },
      { factory_id: 'f-sre', name: 'sre', max: 2 },
      { factory_id: 'f-support', name: 'support', max: null },
    ],
  },
};

describe('changedFields', () => {
  test('is empty while the form matches the settings', () => {
    expect(changedFields(acme, formOf(acme))).toEqual({});
  });

  test('sends only the General fields that changed, trimmed', () => {
    const form = { ...formOf(acme), name: '  Acme Labs ', mark: 'Ac' };
    expect(changedFields(acme, form)).toEqual({ name: 'Acme Labs' });
  });

  test('sends a new address', () => {
    expect(changedFields(acme, { ...formOf(acme), slug: 'acme-labs' })).toEqual({ slug: 'acme-labs' });
  });

  test('a changed factory cap sends the organization cap it must carry, and only that factory', () => {
    const form = formOf(acme);
    form.factories['f-sre'] = '4';
    expect(changedFields(acme, form)).toEqual({ slots: { max: 6, factories: [{ factory_id: 'f-sre', max: 4 }] } });
  });

  test('a blank cap clears it: null, for the organization and for a factory', () => {
    const form = { ...formOf(acme), max: ' ' };
    form.factories['f-goal'] = '';
    expect(changedFields(acme, form)).toEqual({
      slots: { max: null, factories: [{ factory_id: 'f-goal', max: null }] },
    });
  });

  test('a changed organization cap alone sends no factories', () => {
    expect(changedFields(acme, { ...formOf(acme), max: '8' })).toEqual({ slots: { max: 8 } });
  });

  test('setting a cap on an uncapped factory sends it', () => {
    const form = formOf(acme);
    form.factories['f-support'] = '1';
    expect(changedFields(acme, form)).toEqual({ slots: { max: 6, factories: [{ factory_id: 'f-support', max: 1 }] } });
  });
});

describe('validate', () => {
  test('accepts the settings as they are', () => {
    expect(validate(formOf(acme))).toEqual({});
  });

  test('needs a name and a mark', () => {
    expect(validate({ ...formOf(acme), name: '  ', mark: '' })).toEqual({
      name: 'Give the organization a name.',
      mark: 'One or two letters or digits.',
    });
  });

  test('holds the address to goald’s rule and its reserved words', () => {
    const rule = '3 to 40 lowercase letters, digits or dashes, not starting or ending with a dash.';
    for (const slug of ['ac', 'Acme', '-acme', 'acme-', 'ac me', 'a'.repeat(41)]) {
      expect(validate({ ...formOf(acme), slug }).slug, slug).toBe(rule);
    }
    expect(validate({ ...formOf(acme), slug: 'settings' }).slug).toBe('That address is reserved.');
    expect(validate({ ...formOf(acme), slug: 'acme-labs-2' })).toEqual({});
  });

  test('takes a cap of 1 to 1000 or blank', () => {
    const form = { ...formOf(acme), max: '0' };
    form.factories['f-sre'] = '2.5';
    form.factories['f-goal'] = '1001';
    form.factories['f-support'] = '1000';
    const message = 'A whole number from 1 to 1000, or blank for no limit.';
    expect(validate(form)).toEqual({ max: message, 'factory:f-sre': message, 'factory:f-goal': message });
  });
});

describe('slotNote', () => {
  test('adds up the factory limits against the organization’s slots', () => {
    const form = formOf(acme);
    form.factories['f-support'] = '3';
    expect(slotNote(form)).toBe(
      'Limits add up to 8 of 6 slots, so factories wait for each other at peak. That’s allowed.',
    );
    form.factories['f-support'] = '1';
    expect(slotNote(form)).toBe('Limits add up to 6 of 6 slots.');
  });

  test('says when a factory without a limit may take every slot', () => {
    expect(slotNote(formOf(acme))).toBe('A factory without a limit may use all 6 slots.');
  });

  test('says when the organization has no limit', () => {
    expect(slotNote({ ...formOf(acme), max: '' })).toBe(
      'No limit across the organization: each factory stops at its own limit.',
    );
  });

  test('says nothing while a value is not a number', () => {
    expect(slotNote({ ...formOf(acme), max: 'six' })).toBeNull();
  });
});

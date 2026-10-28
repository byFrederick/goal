// A judge's editable fields as the judge page's form holds them, and the JudgeWrite that saves them. The
// file's title is not edited here: the page shows it. A judge's model is absent when the form's model is
// empty, so the factory's runtime model runs it (the contract's rule).
import type { Judge, JudgeOutcome, JudgeWrite } from '../../api/types';

export interface JudgeForm {
  criterion: string;
  agents: string[];
  outcomes: JudgeOutcome[];
  /** 0 to 1, in steps of 0.05. */
  passMark: number;
  /** 0 to 1, in steps of 0.05; 0 pauses the judge. */
  sample: number;
  /** A model id, or empty for the factory's runtime model. */
  model: string;
  enabled: boolean;
  rubric: string;
}

export interface JudgeFormErrors {
  criterion?: string | undefined;
  outcomes?: string | undefined;
}

/** The form a judge's file starts from. */
export function formOf(judge: Judge): JudgeForm {
  return {
    criterion: judge.criterion,
    agents: [...judge.agents],
    outcomes: judge.outcomes.map((o) => ({ ...o })),
    passMark: judge.pass_mark,
    sample: judge.sample,
    model: judge.model ?? '',
    enabled: judge.enabled,
    rubric: judge.rubric,
  };
}

/** What is wrong with the form, by field; empty when it can be saved. */
export function formErrors(form: JudgeForm): JudgeFormErrors {
  const errors: JudgeFormErrors = {};
  if (form.criterion.trim() === '') errors.criterion = 'Say what the judge checks.';
  if (form.outcomes.length === 0) errors.outcomes = 'Add at least one outcome.';
  else if (form.outcomes.some((o) => o.label.trim() === '')) errors.outcomes = 'Every outcome needs a label.';
  return errors;
}

/** The body that saves a judge: its title and the form, with the version the caller saw running. */
export function judgeWrite(title: string, form: JudgeForm, expectedVersionId: string | undefined): JudgeWrite {
  const write: JudgeWrite = {
    title,
    criterion: form.criterion.trim(),
    agents: form.agents,
    outcomes: form.outcomes.map((o) => ({ score: o.score, label: o.label.trim(), meaning: o.meaning.trim() })),
    pass_mark: form.passMark,
    sample: form.sample,
    enabled: form.enabled,
    rubric: form.rubric,
  };
  if (form.model !== '') write.model = form.model;
  if (expectedVersionId !== undefined) write.expected_current_version_id = expectedVersionId;
  return write;
}

/** The body that saves a judge as it is, with only its switch flipped (the list's switch). */
export function judgeToggle(judge: Judge, expectedVersionId: string | undefined): JudgeWrite {
  return judgeWrite(judge.title, { ...formOf(judge), enabled: !judge.enabled }, expectedVersionId);
}

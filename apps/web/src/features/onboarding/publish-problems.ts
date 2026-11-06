// A refused publish (goald's definition-invalid answer) explained on the Budget step: each error problem is
// mapped, by its section and pointer, to the onboarding step that fixes it and a plain sentence. goald's own
// shapes decide the map (internal/config/model.go): there is no `budget` or `repository` section, so budget
// fields come back as `review` with a /review/budget pointer and the repository as `basics` with a
// /basics/repository pointer; a schema error at the top of the file comes back as `files` with no pointer.
import type { DefinitionProblem } from '../../api/types';
import { type OnboardingStepKey, stepLabel } from './steps';

/** The steps a problem can be fixed on. */
export type FixStep = Extract<OnboardingStepKey, 'factory' | 'code' | 'line' | 'budget'>;

export interface PublishProblem {
  /** One plain sentence, without a code or a schema path. */
  sentence: string;
  /** The step that fixes it; undefined when the map does not know the problem. */
  step?: FixStep | undefined;
}

/** The message without its leading schema path: "spec.board: missing property 'stations'" becomes "missing property 'stations'". */
export function withoutSchemaPath(message: string): string {
  return message.replace(/^spec(?:[.[][^:\s]*)?:\s*/, '');
}

const sentenceOf = (text: string) => {
  const trimmed = text.trim();
  const capital = trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(capital) ? capital : `${capital}.`;
};

const missing = (message: string) => /missing property '(\w+)'/.exec(message)?.[1];

function stepOf(problem: DefinitionProblem, missingName: string | undefined): FixStep | undefined {
  const pointer = problem.pointer ?? '';
  switch (problem.section) {
    case 'line':
    case 'agents':
      return 'line';
    case 'basics':
      return pointer.startsWith('/basics/repository') ? 'code' : 'factory';
    case 'review':
      return pointer.startsWith('/review/budget') ? 'budget' : undefined;
    case 'files':
      return problem.code === 'schema-required' &&
        (missingName === 'agents' || missingName === 'board' || missingName === 'stations')
        ? 'line'
        : undefined;
    default:
      return undefined;
  }
}

/** The error problems of a refusal as sentences with the step to fix each; warnings and info are dropped. */
export function explainProblems(problems: DefinitionProblem[]): PublishProblem[] {
  const out: PublishProblem[] = [];
  for (const problem of problems) {
    if (problem.severity !== 'error') continue;
    const message = withoutSchemaPath(problem.message);
    const name = missing(message);
    const step = stepOf(problem, name);
    let sentence: string;
    if (step === 'line' && name === 'stations') sentence = 'The line has no stations. Add one on the Line step.';
    else if (step === 'line' && name === 'agents') sentence = 'The factory has no agents. Add one on the Line step.';
    else if (step === 'line' && name === 'board') sentence = 'The factory has no line. Add a station on the Line step.';
    else if (step === undefined || step === 'budget') sentence = sentenceOf(message);
    else sentence = `${sentenceOf(message)} Fix it on the ${stepLabel(step)} step.`;
    if (!out.some((p) => p.sentence === sentence && p.step === step)) out.push({ sentence, step });
  }
  return out;
}

/** The steps to link to, once each, in the order the problems first named them. Budget is this step itself. */
export function stepsToLink(problems: PublishProblem[]): Exclude<FixStep, 'budget'>[] {
  const steps: Exclude<FixStep, 'budget'>[] = [];
  for (const { step } of problems) {
    if (step && step !== 'budget' && !steps.includes(step)) steps.push(step);
  }
  return steps;
}

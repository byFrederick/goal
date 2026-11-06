// The starter item of the mock's software-delivery template (ADR 0032 decision 3): the template's YAML carries it as
// `starter.item`, and the mock's submit answers for it as the picture shows it (api.ts progressStarter).
export const starterItem = {
  title: 'Add a contributing guide to the README',
  type: 'feature',
  body: 'Add a short contributing guide to README.md, one file and two criteria.',
};

/** The code-review template's starter item: the mock's submit answers for it the same way (api.ts). */
export const reviewStarterItem = {
  title: 'Review the newest open pull request',
  type: 'review',
  body: 'Read the newest open pull request against the team conventions and say what to change, in two or three points.',
};

/** Every title the mock treats as a starter item: it runs its whole course at once. */
export const starterTitles: readonly string[] = [starterItem.title, reviewStarterItem.title];

// The starter item of the mock's software-delivery template (ADR 0032 decision 3): the template's YAML carries it as
// `starter.item`, and the mock's submit answers for it as the picture shows it (api.ts progressStarter).
export const starterItem = {
  title: 'Add a short README section on how to run the tests',
  type: 'feature',
  body: 'Add a "Running the tests" section to the README that says which command runs the tests and what it needs installed. Change nothing else.',
};

/** The incident-response template's starter item, as deploy/catalog/templates commits it. */
export const incidentStarterItem = {
  title: 'Checkout page returns errors for some visitors',
  type: 'incident',
  body: 'Since this morning, some visitors to https://shop.example.com/checkout see an error page. Sort it by severity, find the likely cause and propose a fix.',
};

/** The support-replies template's starter item, as deploy/catalog/templates commits it. */
export const supportStarterItem = {
  title: 'How do I reset my password?',
  type: 'ticket',
  body: 'A customer writes from jo@example.com: "I forgot my password and the reset email never arrives. How do I get back into my account?"',
};

/** The code-review template's starter item: the mock's submit answers for it the same way (api.ts). */
export const reviewStarterItem = {
  title: 'Review the newest open pull request',
  type: 'review',
  body: 'Read the newest open pull request against the team conventions and say what to change, in two or three points.',
};

/** Every title the mock treats as a starter item: it runs its whole course at once. */
export const starterTitles: readonly string[] = [
  starterItem.title,
  reviewStarterItem.title,
  incidentStarterItem.title,
  supportStarterItem.title,
];

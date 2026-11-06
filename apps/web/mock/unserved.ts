// Calls of the web app that the mock deliberately does not serve, checked by coverage.test.ts.

/** 'METHOD path' of a call the mock does not serve, with why. */
export const unserved: Record<string, string> = {
  'DELETE /v1/orgs/{org}/factories/{factory}/intake/triggers/{trigger}/state':
    'Intake trigger state is not mocked; the reset control has no dev data',
  'DELETE /v1/orgs/{org}/items/{item}/start': 'Item start schedules are not mocked',
  'DELETE /v1/orgs/{org}/webhooks/{webhook}': 'Only the organization webhook list is mocked',
  'GET /v1/orgs/{org}/factories/{factory}/intake/triggers/{trigger}/checks': 'Trigger checks are not mocked',
  'GET /v1/orgs/{org}/webhooks/{webhook}':
    'Only the organization webhook list is mocked; one webhook is read through the factory endpoints',
  'GET /v1/orgs/{org}/webhooks/{webhook}/deliveries': 'Only the organization webhook list is mocked',
  'GET /v1/orgs/{org}/webhooks/{webhook}/deliveries/{delivery}': 'Only the organization webhook list is mocked',
  'PATCH /v1/orgs/{org}/webhooks/{webhook}': 'Only the organization webhook list is mocked',
  'POST /v1/orgs/{org}/factories/{factory}/agents/{agent}/model-proposal': 'Model proposals are not mocked',
  'POST /v1/orgs/{org}/factories/{factory}/archive': 'Archiving a factory is not mocked',
  'POST /v1/orgs/{org}/factories/{factory}/draft/model/{section}/revert':
    'Reverting a model section of the draft is not mocked',
  'POST /v1/orgs/{org}/factories/{factory}/intake/triggers/{trigger}/checks': 'Trigger checks are not mocked',
  'POST /v1/orgs/{org}/factories/{factory}/jonah/tests': 'Jonah tests are not mocked',
  'POST /v1/orgs/{org}/factories/{factory}/purge': 'Purging a factory is not mocked',
  'POST /v1/orgs/{org}/factories/{factory}/unarchive': 'Unarchiving a factory is not mocked',
  'POST /v1/orgs/{org}/items/{item}/follow-ups/{followUp}/drop': 'Item follow-ups are not mocked',
  'POST /v1/orgs/{org}/items/{item}/follow-ups/{followUp}/file': 'Item follow-ups are not mocked',
  'POST /v1/orgs/{org}/webhooks': 'Only the organization webhook list is mocked',
  'PUT /v1/orgs/{org}/factories/{factory}/draft/model/{section}': 'Saving a model section of the draft is not mocked',
  'PUT /v1/orgs/{org}/items/{item}/start': 'Item start schedules are not mocked',
};

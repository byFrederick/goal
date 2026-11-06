// Makes an organization secret open to one factory, as the Code step and the model key card do when a name is
// taken: the secret's value is never replaced.
import { getSecret, patchSecret } from '../../api/secrets';

/** Makes the secret open to the factory, keeping its value and every other factory it was open to. */
export async function openSecretTo(org: string, name: string, factory: string): Promise<void> {
  const secret = await getSecret(org, name);
  if (secret.scope.all_factories || secret.scope.factories.includes(factory)) return;
  await patchSecret(org, name, { scope: { all_factories: false, factories: [...secret.scope.factories, factory] } });
}

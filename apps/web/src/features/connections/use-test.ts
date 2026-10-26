// Test (spec 4 §8.2): goald sends the kind's test request with the connection's values and
// answers how it went; a failed test is an answer too (ok false, why), not an error. The outcome
// becomes the connection's last test, so its lists and detail are read again.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { testConnection } from '../../api/connections';
import { keys } from '../../api/queries';
import { refusalText } from './words';

export function useTestConnection(org: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => testConnection(org, name),
    onSuccess: async (result, name) => {
      if (result.ok) toast.success(`${name} passed its test`, { description: testDetail(result.status) });
      else toast.error(`${name} failed its test`, { description: result.error ?? testDetail(result.status) });
      await client.invalidateQueries({ queryKey: keys.connections(org) });
    },
    onError: (err, name) => toast.error(`Couldn’t test ${name}`, { description: refusalText(err) }),
  });
}

const testDetail = (status: number | undefined) => (status ? `It answered ${status}.` : undefined);

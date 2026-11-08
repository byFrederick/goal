import { describe, expect, it } from 'vitest';
import { openPool, poolMax } from './db.js';

describe('openPool', () => {
  it('caps the pool at 4 connections, its share of the connection budget', async () => {
    // pg.Pool connects only on the first query, so this opens no connection.
    const pool = openPool('postgres://goal_auth:test-password@db.example.com:5432/goal');
    try {
      expect(poolMax).toBe(4);
      expect(pool.options.max).toBe(4);
      expect(pool.totalCount).toBe(0);
    } finally {
      await pool.end();
    }
  });
});

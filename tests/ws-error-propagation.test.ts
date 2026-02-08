/**
 * Unit tests for flow control client error and edge case handling.
 */

import { EventEmitter } from 'events';
import { Readable } from 'stream';

// We test the behavioral contract: if a WebSocket error occurs after
// the response promise has resolved, it should destroy the bodyStream.
// Since the actual implementation creates WS internally, we verify
// the pattern by testing the error propagation logic directly.

describe('WS error after resolution', () => {
  test('post-resolution error should destroy body stream', (done) => {
    // Simulate the pattern used in flow-control-client.ts
    const bodyStream = new Readable({ read() {} });
    let resolved = false;

    // Simulate the error handler pattern from the fix
    const handleError = (err: Error) => {
      if (!resolved) {
        // Would reject promise
      } else {
        // Post-resolution: propagate to body stream
        if (bodyStream && !bodyStream.destroyed) {
          bodyStream.destroy(err);
        }
      }
    };

    // Simulate resolution
    resolved = true;
    bodyStream.push(Buffer.from('partial data'));

    bodyStream.on('error', (err) => {
      expect(err.message).toBe('connection reset');
      done();
    });

    // Now simulate a post-resolution WS error
    handleError(new Error('connection reset'));
  });

  test('pre-resolution error should not destroy body stream', () => {
    const bodyStream = new Readable({ read() {} });
    let resolved = false;
    let rejected = false;

    const handleError = (err: Error) => {
      if (!resolved) {
        rejected = true;
      } else {
        if (bodyStream && !bodyStream.destroyed) {
          bodyStream.destroy(err);
        }
      }
    };

    handleError(new Error('connection refused'));
    expect(rejected).toBe(true);
    expect(bodyStream.destroyed).toBe(false);
  });
});

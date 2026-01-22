/**
 * Tests for InstanceManager initialization promise cleanup
 *
 * Bug: initializingPromises map grows unbounded on failure because
 * the promise is only deleted on success path, not on failure.
 *
 * Fix: Use try/finally to ensure cleanup happens regardless of success/failure.
 */

import { _InstanceManager as InstanceManager } from "../dist/index.js";

jest.setTimeout(30000);

describe("InstanceManager initializingPromises cleanup", () => {
  let instanceManager: InstanceType<typeof InstanceManager>;

  beforeEach(() => {
    // Reset singleton between tests
    InstanceManager._resetInstance();
    instanceManager = InstanceManager.getInstance();
  });

  afterEach(async () => {
    // Cleanup any instances that may have been created
    try {
      await instanceManager.cleanup();
    } catch (e) {
      // Ignore cleanup errors
    }
  });

  test("should remove promise from initializingPromises after successful initialization", async () => {
    const port = 9200;

    // Verify no promises initially
    expect(instanceManager._getInitializingPromisesCount()).toBe(0);
    expect(instanceManager._hasInitializingPromise(port)).toBe(false);

    // Start initialization - this should succeed
    const initPromise = instanceManager.getOrCreateSharedInstance(port, false, 10000);

    // During initialization, the promise should be in the map
    expect(instanceManager._hasInitializingPromise(port)).toBe(true);

    // Wait for initialization to complete
    await initPromise;

    // After success, promise should be removed from initializingPromises
    expect(instanceManager._hasInitializingPromise(port)).toBe(false);
    expect(instanceManager._getInitializingPromisesCount()).toBe(0);

    // Clean up the instance
    await instanceManager.removeSharedInstance(port);
  });

  test("concurrent requests to same port should return existing instance after init", async () => {
    const port = 9201;

    // Start first initialization
    const promise1 = instanceManager.getOrCreateSharedInstance(port, false, 10000);

    // Wait for it to complete
    const instance1 = await promise1;

    // Second request should get the same instance
    const promise2 = instanceManager.getOrCreateSharedInstance(port, false, 10000);
    const instance2 = await promise2;

    // Both should be the same instance
    expect(instance1).toBe(instance2);

    // No initializing promises should remain
    expect(instanceManager._hasInitializingPromise(port)).toBe(false);

    // Clean up
    await instanceManager.removeSharedInstance(port);
  });

  test("removing instance clears shared instance but allows fresh port use", async () => {
    const port = 9202;

    // Create initial instance
    const instance1 = await instanceManager.getOrCreateSharedInstance(port, false, 10000);
    expect(instanceManager._hasInitializingPromise(port)).toBe(false);

    // Remove the instance
    await instanceManager.removeSharedInstance(port);

    // After removal, trying to get the same port should start fresh initialization
    // (not return the removed instance)
    const promise2 = instanceManager.getOrCreateSharedInstance(port, false, 10000);

    // Since the instance was removed, this should be a NEW initialization attempt
    // (not returning instance1 which was cleaned up)
    // Note: The actual reinitialization may fail due to port conflicts with the
    // still-running Go process, but the important thing is that:
    // 1. The removed instance is not returned
    // 2. A new initialization is attempted (promise added to map)
    expect(instanceManager._hasInitializingPromise(port)).toBe(true);

    // Wait and clean up - if it fails due to port conflict, that's OK for this test
    try {
      await promise2;
      await instanceManager.removeSharedInstance(port);
    } catch (e) {
      // Port conflict is expected - the important assertion was above
      // Clean up the promise from the map
    }
  });

  test("multiple ports can be initialized independently", async () => {
    const port1 = 9203;
    const port2 = 9204;

    // Start both initializations
    const promise1 = instanceManager.getOrCreateSharedInstance(port1, false, 10000);
    const promise2 = instanceManager.getOrCreateSharedInstance(port2, false, 10000);

    // Both should have initializing promises
    expect(instanceManager._hasInitializingPromise(port1)).toBe(true);
    expect(instanceManager._hasInitializingPromise(port2)).toBe(true);
    expect(instanceManager._getInitializingPromisesCount()).toBe(2);

    // Wait for both
    await Promise.all([promise1, promise2]);

    // Both should be cleaned up
    expect(instanceManager._hasInitializingPromise(port1)).toBe(false);
    expect(instanceManager._hasInitializingPromise(port2)).toBe(false);
    expect(instanceManager._getInitializingPromisesCount()).toBe(0);

    // Clean up
    await instanceManager.removeSharedInstance(port1);
    await instanceManager.removeSharedInstance(port2);
  });
});

/**
 * Test to verify the fix works with actual failure case.
 *
 * The SharedInstance now properly rejects its promise when initialization fails
 * via the rejectInitialization() helper method, which is accessible from
 * spawnServer() and handleSpawn() error handlers.
 */
describe("InstanceManager failure cleanup", () => {
  test("should remove promise from initializingPromises after failed initialization", async () => {
    InstanceManager._resetInstance();
    const instanceManager = InstanceManager.getInstance();
    const port = 9210;
    const invalidExecutablePath = "/nonexistent/path/to/cycletls";

    // Start initialization with invalid path - this should fail
    const initPromise = instanceManager.getOrCreateSharedInstance(
      port,
      false,
      5000,
      invalidExecutablePath
    );

    // Wait for initialization to fail - should reject with error message, not timeout
    await expect(initPromise).rejects.toMatch(/Executable not found|not found/i);

    // After failure, promise should be removed from initializingPromises
    expect(instanceManager._hasInitializingPromise(port)).toBe(false);
  });

  test("should reject immediately on spawn error, not wait for timeout", async () => {
    InstanceManager._resetInstance();
    const instanceManager = InstanceManager.getInstance();
    const port = 9211;
    const invalidExecutablePath = "/nonexistent/path/to/cycletls";

    const startTime = Date.now();

    // Start initialization with invalid path and long timeout
    const initPromise = instanceManager.getOrCreateSharedInstance(
      port,
      false,
      30000, // 30 second timeout - we should NOT wait this long
      invalidExecutablePath
    );

    // Wait for initialization to fail
    await expect(initPromise).rejects.toMatch(/Executable not found|not found/i);

    const elapsed = Date.now() - startTime;

    // Should fail quickly (well under 30 seconds), not wait for timeout
    // Allow 5 seconds for test overhead
    expect(elapsed).toBeLessThan(5000);
  });
});

/**
 * Tests for zombie process prevention on early initialization rejection.
 *
 * Bug: When rejectInitialization() is called, the spawned child process
 * is not killed, leaving zombie processes.
 *
 * Fix: Kill the child process in rejectInitialization() before rejecting.
 */

describe("Zombie process prevention", () => {
  test("rejectInitialization source should kill child process", () => {
    const fs = require("fs");
    const source = fs.readFileSync(
      require.resolve("../dist/index.js"),
      "utf8"
    );

    // Find the rejectInitialization method
    const methodStart = source.indexOf("rejectInitialization(reason)");
    const methodEnd = source.indexOf("}", methodStart + 100);
    const methodSource = source.substring(methodStart, methodEnd + 50);

    // The method should check for and kill the child process
    expect(methodSource).toContain("this.child");
    expect(methodSource).toContain("kill");
  });

  test("rejectInitialization should handle missing child process gracefully", () => {
    const fs = require("fs");
    const source = fs.readFileSync(
      require.resolve("../dist/index.js"),
      "utf8"
    );

    // Find the rejectInitialization method - use a wider search range
    // to capture the full method body including all branches
    const methodStart = source.indexOf("rejectInitialization(reason)");
    const nextMethod = source.indexOf("initialize()", methodStart);
    const methodSource = source.substring(methodStart, nextMethod);

    // Should have a null check for child process
    expect(methodSource).toMatch(/this\.child\b/);
    // Should set child to null after killing
    expect(methodSource).toContain("this.child = null");
  });

  test("rejectInitialization should handle process group kill on unix", () => {
    const fs = require("fs");
    const source = fs.readFileSync(
      require.resolve("../dist/index.js"),
      "utf8"
    );

    const methodStart = source.indexOf("rejectInitialization(reason)");
    // Find a broader region to capture the full method body
    const nextMethod = source.indexOf("initialize()", methodStart);
    const methodSource = source.substring(methodStart, nextMethod);

    // On non-Windows, should use process.kill with negative PID for process group
    expect(methodSource).toContain("process.kill");
    // Should handle both Windows and Unix
    expect(methodSource).toContain("win32");
  });
});

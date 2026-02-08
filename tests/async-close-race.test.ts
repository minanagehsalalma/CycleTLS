/**
 * Tests for HTTP server async close race condition.
 */

describe("HTTP server async close race", () => {
  test("error handler should call createClient inside close callback", () => {
    const fs = require("fs");
    const source = fs.readFileSync(
      require.resolve("../dist/index.js"),
      "utf8"
    );

    const methodStart = source.indexOf("checkSpawnedInstance(resolve, reject) {");
    const nextMethodIdx = source.indexOf("spawnServer() {", methodStart);
    const methodSource = source.substring(methodStart, nextMethodIdx);

    // Find error handler using string concat to avoid nested quote issues
    const errorPattern = "once(" + String.fromCharCode(39) + "error" + String.fromCharCode(39);
    const errorIdx = methodSource.indexOf(errorPattern);
    expect(errorIdx).toBeGreaterThan(-1);
    const errorHandler = methodSource.substring(errorIdx);

    // createClient should be inside the close callback
    const closeCallbackStart = errorHandler.indexOf(".close(()");
    expect(closeCallbackStart).toBeGreaterThan(-1);

    const afterClose = errorHandler.substring(closeCallbackStart);
    const createClientPos = afterClose.indexOf("createClient");
    expect(createClientPos).toBeGreaterThan(0);

    const closingBracePos = afterClose.indexOf("});");
    expect(createClientPos).toBeLessThan(closingBracePos);
  });

  test("else branch should also call createClient when httpServer is null", () => {
    const fs = require("fs");
    const source = fs.readFileSync(
      require.resolve("../dist/index.js"),
      "utf8"
    );

    const methodStart = source.indexOf("checkSpawnedInstance(resolve, reject) {");
    const nextMethodIdx = source.indexOf("spawnServer() {", methodStart);
    const methodSource = source.substring(methodStart, nextMethodIdx);

    const errorPattern = "once(" + String.fromCharCode(39) + "error" + String.fromCharCode(39);
    const errorIdx = methodSource.indexOf(errorPattern);
    const errorHandler = methodSource.substring(errorIdx);

    // Count createClient occurrences - should be >= 2
    const matches = errorHandler.match(/createClient/g) || [];
    expect(matches.length).toBeGreaterThanOrEqual(2);

    // Should have an else branch
    expect(errorHandler).toContain("else");
  });
});
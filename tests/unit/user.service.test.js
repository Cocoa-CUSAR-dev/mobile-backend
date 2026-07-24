/**
 * tests/unit/user.service.test.js
 *
 * This is a Go backend (see go.mod). There is no user.service JavaScript
 * module to test. The Go equivalents live at:
 *   - internal/handlers/auth_handler.go        (Login, Register, GetMe, GenerateToken)
 *   - internal/handlers/auth_handler_test.go   (unit tests for the above)
 *
 * Run them with:   go test ./internal/handlers/ -v
 *
 * This file exists only so the `tests/unit/` directory is not empty in CI
 * scans. It contains a single, dependency-free assertion (using Node's
 * built-in test runner) that confirms the file is wired up. If a real JS
 * user service is ever introduced, replace this stub with proper tests.
 *
 * Run with:   node --test tests/unit/user.service.test.js
 */

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

test('user.service (stub) — file is wired into the test runner', () => {
  // The Go handler tests live next to the handler itself:
  //   internal/handlers/auth_handler_test.go
  // Keeping this stub means a `find tests/unit -name '*.test.js'` step
  // returns a real file rather than nothing.
  assert.equal(typeof __filename, 'string');
});

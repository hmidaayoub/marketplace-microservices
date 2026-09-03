/**
 * Component-suite setup.
 *
 * jest-dom for the matchers that describe a DOM node in the terms the assertion is
 * actually about - toBeInTheDocument, toHaveAttribute - rather than through
 * `expect(node !== null).toBe(true)`.
 *
 * The cleanup is the part that is easy to leave out and expensive to debug: Testing
 * Library renders into a container appended to document.body, and without an unmount
 * between tests the second `getByRole` in a file can match the first test's markup and
 * pass for the wrong reason.
 */

import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(cleanup)

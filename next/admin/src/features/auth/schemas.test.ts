import { describe, expect, it } from 'vitest'
import {
	loginFormSchema,
	normalizeLoginValues,
	normalizePasswordResetRequestValues,
	normalizePasswordResetValues,
	passwordResetFormSchema,
	passwordResetRequestFormSchema,
	normalizeSetupValues,
	setupFormSchema,
} from './schemas'

const password = 'Admin1!x'

function setupValues(value: string) {
  return {
    name: 'Admin',
    email: 'admin@example.com',
    password: value,
    passwordConfirmation: value,
  }
}

describe('auth form schemas', () => {
  it('normalizes names and email identifiers but keeps password whitespace', () => {
    const values = {
      name: '  Jo\u0301  ',
      email: '  admin@example.com  ',
      password,
      passwordConfirmation: password,
    }
    expect(setupFormSchema.safeParse(values).success).toBe(true)
    expect(normalizeSetupValues(values)).toEqual({
      name: 'Jó',
      email: 'admin@example.com',
      password,
    })
    expect(normalizeLoginValues({ email: values.email, password: ` ${password} ` })).toEqual({
      email: 'admin@example.com',
      password: ` ${password} `,
    })
  })

  it('checks code point length, controls, email shape and confirmation', () => {
    const result = setupFormSchema.safeParse({
      name: `\u0000${'😀'.repeat(101)}`,
      email: 'bad@@example..com',
      password: 'short',
      passwordConfirmation: 'different',
    })
    expect(result.success).toBe(false)
    if (result.success) return
    const codes = result.error.issues.map((issue) => issue.message)
    expect(codes).toEqual(expect.arrayContaining(['invalid_name', 'invalid_email', 'invalid_password', 'password_mismatch']))
  })

  it('enforces the creation length and ASCII character classes', () => {
    expect(setupFormSchema.safeParse(setupValues('Aa1!xxx')).success).toBe(false)
    expect(setupFormSchema.safeParse(setupValues('Aa1!xxxx')).success).toBe(true)
    expect(setupFormSchema.safeParse(setupValues(`Aa1!${'x'.repeat(124)}`)).success).toBe(true)
    expect(setupFormSchema.safeParse(setupValues(`Aa1!${'x'.repeat(125)}`)).success).toBe(false)

    for (const value of ['aa1!xxxx', 'AA1!XXXX', 'Aax!xxxx', 'Aa1xxxxx', 'aá1!xxxx', 'AÁ1!XXXX', 'Aa１!xxxx', 'Aa1！xxxx']) {
      expect(setupFormSchema.safeParse(setupValues(value)).success, value).toBe(false)
    }
    expect(setupFormSchema.safeParse(setupValues('Aa1!😀xxx')).success).toBe(true)
  })

  it('keeps login validation compatible with legacy passwords', () => {
    expect(loginFormSchema.safeParse({ email: 'admin@example.com', password: 'correct horse battery' }).success).toBe(true)
    expect(loginFormSchema.safeParse({ email: 'admin@example.com', password: 'password' }).success).toBe(true)
    expect(loginFormSchema.safeParse({ email: 'admin@example.com', password: '' }).success).toBe(false)
    expect(loginFormSchema.safeParse({ email: 'admin@example.com', password: '😀'.repeat(128) }).success).toBe(true)
    expect(loginFormSchema.safeParse({ email: 'admin@example.com', password: '😀'.repeat(129) }).success).toBe(false)
  })

	it('compares passwords after the same NFC normalization sent to the API', () => {
    const result = setupFormSchema.safeParse({
      name: 'Admin',
      email: 'admin@example.com',
      password: 'Aa1!e\u0301xxx',
      passwordConfirmation: 'Aa1!éxxx',
    })
    expect(result.success).toBe(true)
	})

	it('validates recovery email, password confirmation, and normalization', () => {
		expect(passwordResetRequestFormSchema.safeParse({ email: '  admin@example.com  ' }).success).toBe(true)
		expect(normalizePasswordResetRequestValues({ email: '  admin@example.com  ' })).toEqual({ email: 'admin@example.com' })
		expect(passwordResetRequestFormSchema.safeParse({ email: 'bad' }).success).toBe(false)
		expect(passwordResetFormSchema.safeParse({ password: 'Aa1!xxxx', passwordConfirmation: 'Aa1!xxx' }).success).toBe(false)
		expect(passwordResetFormSchema.safeParse({ password: 'Aa1!e\u0301xxx', passwordConfirmation: 'Aa1!éxxx' }).success).toBe(true)
		expect(normalizePasswordResetValues({ password: 'Aa1!e\u0301xxx', passwordConfirmation: 'Aa1!éxxx' })).toEqual({ password: 'Aa1!éxxx' })
	})
})

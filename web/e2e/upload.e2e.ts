import { expect, test } from '@playwright/test'

test('uploads a text file and reaches a terminal status', async ({ page }) => {
  const title = `Nets ${Date.now()}`
  await page.goto('/')
  await page.getByRole('button', { name: 'Courses' }).click()
  await page.getByLabel('Course title').fill(title)
  await page.getByRole('button', { name: 'Create course' }).click()
  await expect(page.getByRole('heading', { name: title })).toBeVisible()

  await page.getByLabel('Upload files').setInputFiles({
    name: 'notes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('hello notes\n'),
  })

  await expect(page.getByText('notes.txt')).toBeVisible()
  await expect(page.getByText(/Ready|Failed/)).toBeVisible({ timeout: 60_000 })
})

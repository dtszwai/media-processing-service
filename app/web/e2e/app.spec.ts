import { test, expect } from '@playwright/test';

test.describe('Media Processing App', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
  });

  test('shows header with title', async ({ page }) => {
    await expect(page.locator('header')).toContainText('Media Processing Service');
  });

  test('shows API connection status', async ({ page }) => {
    const status = page.locator('header').getByText(/Connected|Disconnected/);
    await expect(status).toBeVisible();
  });

  test('shows upload zone', async ({ page }) => {
    await expect(page.getByText('Upload Image')).toBeVisible();
    await expect(page.getByText('Drop image here or click to browse')).toBeVisible();
  });

  test('shows media list section', async ({ page }) => {
    await expect(page.getByText('All Media')).toBeVisible();
  });

  test('upload zone accepts drag over', async ({ page }) => {
    const uploadZone = page.locator('.upload-zone');
    await expect(uploadZone).toBeVisible();
  });

  test('width slider has correct range', async ({ page }) => {
    const slider = page.locator('#widthSlider');
    await expect(slider).toHaveAttribute('min', '100');
    await expect(slider).toHaveAttribute('max', '1024');
  });

  test('process button is disabled without file', async ({ page }) => {
    const button = page.getByRole('button', { name: 'Process Image' });
    await expect(button).toBeDisabled();
  });
});

test.describe('File Selection', () => {
  test('shows preview after selecting image', async ({ page }) => {
    await page.goto('/');

    const fileInput = page.locator('input[type="file"]');
    await fileInput.setInputFiles({
      name: 'test.jpg',
      mimeType: 'image/jpeg',
      buffer: Buffer.from('fake-image-data'),
    });

    await expect(page.getByText('test.jpg')).toBeVisible();
  });

  test('enables process button after file selection', async ({ page }) => {
    await page.goto('/');

    const fileInput = page.locator('input[type="file"]');
    await fileInput.setInputFiles({
      name: 'test.png',
      mimeType: 'image/png',
      buffer: Buffer.from('fake-image-data'),
    });

    const button = page.getByRole('button', { name: 'Process Image' });
    await expect(button).toBeEnabled();
  });

  test('shows large file indicator for files over 5MB', async ({ page }) => {
    await page.goto('/');

    const fileInput = page.locator('input[type="file"]');
    const largeBuffer = Buffer.alloc(6 * 1024 * 1024); // 6MB
    await fileInput.setInputFiles({
      name: 'large.jpg',
      mimeType: 'image/jpeg',
      buffer: largeBuffer,
    });

    await expect(page.getByText('Large file - will use direct S3 upload')).toBeVisible();
  });
});

test.describe('Width Slider', () => {
  test('updates displayed value when slider changes', async ({ page }) => {
    await page.goto('/');

    const slider = page.locator('#widthSlider');
    await slider.fill('750');

    await expect(page.getByText('750px')).toBeVisible();
  });
});

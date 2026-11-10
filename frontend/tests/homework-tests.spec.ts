import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

for (const successful of [false, true]) {
test(`homework has one final attempt (${successful ? 'correct' : 'incorrect'}) and one mark per activity`, async ({ page }) => {
  let role = 'methodist';
  let homework: Record<string, unknown> | null = null;
  let completed = false;
  let attempts = 0;
  let correct = 0;
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    const json = (body: unknown, status = 200) => route.fulfill({ json: body, status });
    if (path === '/features') return json({});
    if (path === '/auth/csrf') return json({ csrf_token: 'fixture' });
    if (path === '/auth/me') return json({ id: 7, name: 'Пользователь AIQ', email: 'user@example.test', roles: [role], must_change_password: false });
    if (path === '/homework-tests/students') return json({ items: [{ id: 10, name: 'Ученик AIQ' }] });
    if (path === '/homework-tests' && route.request().method() === 'POST') {
      const data = route.request().postDataJSON();
      expect(data.student_ids).toEqual([10]);
      expect(data.questions).toEqual([{ prompt: 'Сколько будет 2 + 2?', options: ['3', '4'], correct: 1 }]);
      homework = { id: 20, title: data.title, instructions: data.instructions, questions: data.questions.map(({ prompt, options }: { prompt: string; options: string[] }) => ({ prompt, options })), student_name: 'Ученик AIQ', author_name: 'Методист AIQ', answers: [], due_at: null };
      return json({ id: 1, assigned: 1 }, 201);
    }
    if (path === '/homework-tests/20/submit') {
      const answers = route.request().postDataJSON().answers;
      attempts++; correct = answers[0] === 1 ? 1 : 0; completed = correct === 1;
      if (homework) homework.answers = answers;
      return json({ completed, awarded: completed, correct_count: correct, total: 1 });
    }
    if (path === '/homework-tests') return json({ items: homework ? [{ ...homework, attempts, correct_count: correct, can_submit: role === 'student' && attempts === 0, completed_at: completed ? '2026-11-08T12:00:00Z' : null }] : [], total: homework ? 1 : 0 });
    if (path === '/learning/progress') return json({ items: [{ student_id: 10, student_name: 'Ученик AIQ', total: 2, reviewed: completed ? 2 : 1, assigned: attempts ? 0 : 1, submitted: 0, completed_tests: completed ? 1 : 0, accepted_assignments: 1, completed_lessons: 1, progress_marks: completed ? 3 : 2, failed_tests: attempts && !completed ? 1 : 0 }] });
    return json({ items: [], total: 0 });
  });
  await page.goto('/#/homework-tests');
  await page.getByRole('button', { name: 'Создать тест', exact: true }).click();
  await page.getByLabel('Название теста', { exact: true }).fill('Математика дома');
  await page.getByLabel('Текст вопроса 1', { exact: true }).fill('Сколько будет 2 + 2?');
  await page.getByLabel('Вопрос 1 · вариант 1', { exact: true }).fill('3');
  await page.getByLabel('Вопрос 1 · вариант 2', { exact: true }).fill('4');
  await page.getByLabel('Правильный ответ на вопрос 1', { exact: true }).selectOption('1');
  await page.getByRole('button', { name: 'Выдать тест как ДЗ', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Выберите хотя бы одного ученика');
  expect(homework).toBeNull();
  await page.getByRole('button', { name: 'Выбрать всех', exact: true }).click();
  await expect(page.getByRole('checkbox', { name: 'Ученик AIQ', exact: true })).toBeChecked();
  await page.getByRole('button', { name: 'Выдать тест как ДЗ · ученикам: 1', exact: true }).click();
  await expect(page.getByText('Тест создан и выдан выбранным ученикам.', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Математика дома', exact: true })).toBeVisible();
  role = 'curator'; await page.reload();
  await expect(page.getByRole('button', { name: 'Создать тест', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Создать тест', exact: true }).click();
  await page.getByLabel('Название теста', { exact: true }).fill('Математика от куратора');
  await page.getByLabel('Текст вопроса 1', { exact: true }).fill('Сколько будет 2 + 2?');
  await page.getByLabel('Вопрос 1 · вариант 1', { exact: true }).fill('3');
  await page.getByLabel('Вопрос 1 · вариант 2', { exact: true }).fill('4');
  await page.getByLabel('Правильный ответ на вопрос 1', { exact: true }).selectOption('1');
  await page.getByRole('checkbox', { name: 'Ученик AIQ', exact: true }).check();
  await page.getByRole('button', { name: 'Выдать тест как ДЗ · ученикам: 1', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Математика от куратора', exact: true })).toBeVisible();
  role = 'student'; await page.reload();
  await expect(page.getByRole('button', { name: 'Создать тест', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Выполнить тест', exact: true }).click();
  await expect(page.getByText('У тебя одна попытка.', { exact: false })).toBeVisible();
  await page.getByRole('radio', { name: successful ? '4' : '3', exact: true }).check();
  await page.getByRole('button', { name: 'Проверить ответы', exact: true }).click();
  await expect(page.getByText(successful ? 'Тест выполнен! +1 палочка прогресса.' : 'Тест завершён с ошибками. Ответы сохранены, изменить их нельзя.', { exact: false })).toBeVisible();
  await expect(page.getByText(`Палочек прогресса: ${successful ? 3 : 2}`, { exact: true })).toBeVisible();
  await expect(page.getByRole('progressbar')).toHaveAttribute('aria-valuenow', successful ? '3' : '2');
  await expect(page.getByRole('button', { name: 'Проверить ответы', exact: true })).toHaveCount(0);
  await expect(page.getByRole('radio', { name: '4', exact: true })).toBeDisabled();
  await page.reload();
  await page.getByRole('button', { name: 'Посмотреть тест', exact: true }).click();
  await expect(page.getByRole('radio', { name: successful ? '4' : '3', exact: true })).toBeChecked();
  await expect(page.getByRole('radio', { name: '4', exact: true })).toBeDisabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
});
}

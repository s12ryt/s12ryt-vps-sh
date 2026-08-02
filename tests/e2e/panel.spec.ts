import { expect, test, type Page } from '@playwright/test';

const panelPath = '/abcdefghijkl';

async function login(page: Page): Promise<void> {
  await page.goto(panelPath);
  await page.getByLabel('管理密碼').fill('panel-password');
  await page.getByRole('button', { name: '登入' }).click();
  await expect(page.getByRole('heading', { name: 's12ryt 多 IPv6 出站' })).toBeVisible();
}

test('登入錯誤與成功路徑都可觀察', async ({ page }) => {
  await page.goto(panelPath);
  await expect(page.getByRole('heading', { name: '登入 IPv6 管理面板' })).toBeVisible();
  await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)).toBe('dark');
  await page.getByLabel('管理密碼').fill('wrong-password');
  await page.getByRole('button', { name: '登入' }).click();
  await expect(page.locator('body')).toContainText('密碼錯誤');

  await login(page);
  await expect(page).toHaveURL(new RegExp(`${panelPath}#strategy$`));
});

test('導覽順序與 static backdrop 契約成立', async ({ page }) => {
  await login(page);
  await expect(page.locator('[data-strategy-actions] button')).toHaveText(['出口模式', '拓撲', '協議']);

  await page.getByRole('button', { name: '出口模式' }).click();
  const modal = page.locator('[data-modal="routing"]');
  await expect(modal).toBeVisible();
  await modal.click({ position: { x: 5, y: 5 } });
  await expect(modal).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(modal).toBeHidden();
});

test('五工作區支援 hash、瀏覽器歷史與響應式導覽', async ({ page }, testInfo) => {
  await login(page);
  const tabs = page.getByRole('tablist', { name: '工作區導覽' }).getByRole('tab');
  await expect(tabs).toHaveText(['策略', '節點', '遠端出口', '網路', '分享']);
  await expect(page.getByRole('tabpanel')).toHaveCount(1);
  await expect(page.getByRole('tabpanel', { name: '策略' })).toBeVisible();

  await page.getByRole('tab', { name: '節點' }).click();
  await expect(page).toHaveURL(new RegExp(`${panelPath}#nodes$`));
  await expect(page.getByRole('tabpanel')).toHaveCount(1);
  await expect(page.getByRole('tabpanel', { name: '節點' })).toBeVisible();

  await page.getByRole('tab', { name: '網路' }).click();
  await expect(page).toHaveURL(new RegExp(`${panelPath}#network$`));
  await expect(page.getByRole('tabpanel', { name: '網路' })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`${panelPath}#nodes$`));
  await expect(page.getByRole('tabpanel', { name: '節點' })).toBeVisible();
  await page.goForward();
  await expect(page.getByRole('tabpanel', { name: '網路' })).toBeVisible();

  await page.goto(`${panelPath}#shares`);
  await expect(page.getByRole('tabpanel', { name: '分享' })).toBeVisible();
  await expect(page.getByRole('tabpanel')).toHaveCount(1);

  const navigation = await page.locator('.workspace-nav').evaluate((element) => {
    const style = getComputedStyle(element);
    return {
      flexDirection: style.flexDirection,
      overflowX: style.overflowX,
      position: style.position,
    };
  });
  const theme = await page.evaluate(() => ({
    colorScheme: getComputedStyle(document.documentElement).colorScheme,
    background: getComputedStyle(document.body).backgroundColor,
  }));
  expect(theme.colorScheme).toBe('dark');
  expect(theme.background).not.toBe('rgb(255, 255, 255)');
  if (testInfo.project.name === 'mobile-chromium') {
    expect(navigation.flexDirection).toBe('row');
    expect(['auto', 'scroll']).toContain(navigation.overflowX);
  } else {
    expect(navigation.flexDirection).toBe('column');
    expect(navigation.position).toBe('sticky');
  }
});

test('鍵盤可跳至主內容且 modal 會鎖定並返回焦點', async ({ page }) => {
  await login(page);

  await page.keyboard.press('Tab');
  const skipLink = page.getByRole('link', { name: '跳到主要內容' });
  await expect(skipLink).toBeFocused();
  const focusRing = await skipLink.evaluate((element) => {
    const style = getComputedStyle(element);
    return { style: style.outlineStyle, width: Number.parseFloat(style.outlineWidth) };
  });
  expect(focusRing.style).not.toBe('none');
  expect(focusRing.width).toBeGreaterThan(0);
  await page.keyboard.press('Enter');
  await expect(page.locator('#main-content')).toBeFocused();

  const trigger = page.getByRole('button', { name: '出口模式' });
  await trigger.focus();
  await trigger.click();
  const modal = page.locator('[data-modal="routing"]');
  await expect(modal).toBeVisible();
  await expect(modal.getByRole('radio').first()).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect.poll(() => modal.evaluate((element) => element.contains(document.activeElement))).toBe(true);
  await page.keyboard.press('Escape');
  await expect(modal).toBeHidden();
  await expect(trigger).toBeFocused();

  await trigger.click();
  await modal.getByRole('button', { name: '取消' }).click();
  await expect(modal).toBeHidden();
  await expect(trigger).toBeFocused();
});

test('modal 開啟時背景不可互動且關閉後恢復', async ({ page }) => {
  await login(page);
  const background = page.locator('.dashboard-frame');
  const trigger = page.getByRole('button', { name: '出口模式' });

  await trigger.click();
  await expect(page.locator('[data-modal="routing"]')).toBeVisible();
  await expect(background).toHaveJSProperty('inert', true);

  await page.keyboard.press('Escape');
  await expect(background).toHaveJSProperty('inert', false);
  await expect(trigger).toBeFocused();
});

test('開啟另一個 modal 會先關閉目前 modal 並返回新觸發控制', async ({ page }) => {
  await login(page);
  const routingTrigger = page.getByRole('button', { name: '出口模式' });
  const topologyTrigger = page.getByRole('button', { name: '拓撲' });
  const routingModal = page.locator('[data-modal="routing"]');
  const topologyModal = page.locator('[data-modal="topology"]');

  await routingTrigger.click();
  await topologyTrigger.evaluate((button: HTMLButtonElement) => button.click());
  await expect(routingModal).toBeHidden();
  await expect(topologyModal).toBeVisible();
  await expect(page.locator('[data-modal]:visible')).toHaveCount(1);

  await page.keyboard.press('Escape');
  await expect(topologyTrigger).toBeFocused();
});

test('策略套用在驗證與套用請求期間維持單一 busy 生命週期', async ({ page }) => {
  let validationRequests = 0;
  let applyRequests = 0;
  let releaseValidation: (() => void) | undefined;
  let releaseApply: (() => void) | undefined;
  const validationPending = new Promise<void>((resolve) => { releaseValidation = resolve; });
  const applyPending = new Promise<void>((resolve) => { releaseApply = resolve; });
  await page.route(`**${panelPath}/api/config/validate`, async (route) => {
    validationRequests += 1;
    await validationPending;
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"valid":true,"changed":true}' });
  });
  await page.route(`**${panelPath}/api/config/apply`, async (route) => {
    applyRequests += 1;
    await applyPending;
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"applied":true}' });
  });
  page.on('dialog', (dialog) => dialog.accept());
  await login(page);
  await page.getByRole('button', { name: '出口模式' }).click();
  const apply = page.locator('[data-modal="routing"] [data-config-save]');

  await apply.click();
  try {
    await expect.poll(() => validationRequests).toBe(1);
    await expect(apply).toBeDisabled();
    await expect(apply).toHaveAttribute('aria-busy', 'true');
    await apply.evaluate((button: HTMLButtonElement) => button.click());
    await expect.poll(() => validationRequests).toBe(1);

    releaseValidation?.();
    await expect.poll(() => applyRequests).toBe(1);
    await expect(apply).toBeDisabled();
    await expect(apply).toHaveAttribute('aria-busy', 'true');
  } finally {
    releaseValidation?.();
    releaseApply?.();
  }
  await expect(apply).toBeEnabled();
  await expect(apply).not.toHaveAttribute('aria-busy', 'true');
});

const managedButtonCases = [
  {
    name: '節點刪除',
    workspace: '節點',
    listPath: 'nodes',
    listBody: [{ id: 'edge-test', protocol: 'vless', port: 24443, enabled: true, credential_configured: true }],
    selector: '[data-node-delete]',
    mutationMethod: 'DELETE',
    mutationPath: 'nodes/edge-test',
    responseStatus: 204,
    responseBody: '',
  },
  {
    name: '遠端啟停',
    workspace: '遠端出口',
    listPath: 'remotes',
    listBody: [{ tag: 'remote-test', type: 'vless', server: 'proxy.example.com', port: 443, enabled: true, ipv4_fallback_position: 0 }],
    selector: '[data-remote-toggle]',
    mutationMethod: 'PATCH',
    mutationPath: 'remotes/remote-test',
    responseStatus: 200,
    responseBody: JSON.stringify({ tag: 'remote-test', type: 'vless', server: 'proxy.example.com', port: 443, enabled: false, ipv4_fallback_position: 0 }),
  },
  {
    name: '遠端刪除',
    workspace: '遠端出口',
    listPath: 'remotes',
    listBody: [{ tag: 'remote-test', type: 'vless', server: 'proxy.example.com', port: 443, enabled: true, ipv4_fallback_position: 0 }],
    selector: '[data-remote-delete]',
    mutationMethod: 'DELETE',
    mutationPath: 'remotes/remote-test',
    responseStatus: 204,
    responseBody: '',
  },
] as const;

for (const managedCase of managedButtonCases) {
  test(`${managedCase.name}按鈕會標示 busy、停用並防止重複請求`, async ({ page }) => {
    let mutationRequests = 0;
    let releaseMutation: (() => void) | undefined;
    const mutationPending = new Promise<void>((resolve) => { releaseMutation = resolve; });
    await page.route(`**${panelPath}/api/${managedCase.listPath}`, (route) => route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(managedCase.listBody),
    }));
    await page.route(`**${panelPath}/api/${managedCase.mutationPath}`, async (route) => {
      if (route.request().method() !== managedCase.mutationMethod) {
        await route.fallback();
        return;
      }
      mutationRequests += 1;
      await mutationPending;
      await route.fulfill({
        status: managedCase.responseStatus,
        contentType: managedCase.responseBody ? 'application/json' : undefined,
        body: managedCase.responseBody,
      });
    });
    page.on('dialog', (dialog) => dialog.accept());
    await login(page);
    await page.getByRole('tab', { name: managedCase.workspace }).click();
    const action = page.locator(managedCase.selector).first();
    await expect(action).toBeVisible();

    await action.click();
    try {
      await expect.poll(() => mutationRequests).toBe(1);
      await expect(action).toBeDisabled();
      await expect(action).toHaveAttribute('aria-busy', 'true');
      await action.evaluate((button: HTMLButtonElement) => button.click());
      await expect.poll(() => mutationRequests).toBe(1);
    } finally {
      releaseMutation?.();
    }
    await expect(page.locator(managedCase.selector).first()).toBeEnabled();
    await expect(page.locator(managedCase.selector).first()).not.toHaveAttribute('aria-busy', 'true');
  });
}

test('缺少 CSRF 的狀態變更會被拒絕', async ({ page }) => {
  await login(page);
  const status = await page.evaluate(async () => {
    const shell = document.querySelector<HTMLElement>('.shell');
    const response = await fetch(shell?.dataset.validateEndpoint ?? '', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    });
    return response.status;
  });
  expect(status).toBe(403);
});

test('分享需重新驗證且關閉時清除秘密', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], {
    origin: 'http://127.0.0.1:18080',
  });
  await login(page);
  await page.getByRole('tab', { name: '分享' }).click();
  await page.getByRole('button', { name: '驗證並查看分享' }).click();
  const modal = page.locator('[data-modal="share-reveal"]');
  await modal.getByLabel('管理密碼').fill('panel-password');
  await modal.getByRole('button', { name: '驗證並揭露' }).click();

  const uri = modal.locator('[data-share-uri]');
  await expect(uri).toContainText('vless://');
  await expect(modal.locator('[data-share-expiry]')).toContainText(/(?:[1-9]\d?|[12]\d{2}|300) 秒/);
  const image = modal.locator('img[data-share-qr]');
  await expect(image).toBeVisible();
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => element.naturalWidth)).toBeGreaterThan(0);

  await uri.locator('..').getByRole('button', { name: '複製' }).click();
  const clipboard = await page.evaluate(() => navigator.clipboard.readText());
  expect(clipboard).toContain('vless://');

  await modal.getByRole('button', { name: '取消' }).click();
  await expect(modal).toBeHidden();
  await expect(page.locator('[data-share-uri]')).toHaveCount(0);
  await expect(page.locator('img[data-share-qr]')).toHaveCount(0);
});

test('狀態變更會停用控制、防止重複提交並可在失敗後重試', async ({ page }) => {
  await login(page);
  await page.getByRole('tab', { name: '分享' }).click();
  await page.getByRole('button', { name: '驗證並查看分享' }).click();

  const modal = page.locator('[data-modal="share-reveal"]');
  const form = modal.locator('[data-share-form]');
  const submit = modal.getByRole('button', { name: '驗證並揭露' });
  const notice = page.locator('[data-operation-notice]');
  let requests = 0;
  let releaseFirst: (() => void) | undefined;
  const firstRequest = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });

  await page.route(`**${panelPath}/api/shares/reveal`, async (route) => {
    requests += 1;
    if (requests === 1) {
      await firstRequest;
      await route.fulfill({ status: 503, contentType: 'text/plain', body: '暫時無法取得分享' });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ nodes: [], subscription: '', expires_in_seconds: 300 }),
    });
  });

  await modal.getByLabel('管理密碼').fill('panel-password');
  await submit.click();
  try {
    await expect(form).toHaveAttribute('aria-busy', 'true');
    await expect(submit).toBeDisabled();
    await expect(notice).toContainText('處理中');
    await submit.evaluate((button: HTMLButtonElement) => button.click());
    await expect.poll(() => requests).toBe(1);
  } finally {
    releaseFirst?.();
  }

  await expect(submit).toBeEnabled();
  await expect(form).not.toHaveAttribute('aria-busy', 'true');
  await expect(modal.locator('[data-share-error]')).toContainText('暫時無法取得分享');
  await expect(notice).toContainText('操作失敗');

  await modal.getByLabel('管理密碼').fill('panel-password');
  await submit.click();
  await expect.poll(() => requests).toBe(2);
  await expect(submit).toBeEnabled();
  await expect(notice).toContainText('操作已完成');
});

test('節點監聽錯誤會關聯欄位、移動焦點並可在修正後清除', async ({ page }) => {
  await login(page);
  await page.getByRole('tab', { name: '節點' }).click();
  await page.getByRole('button', { name: '新增節點' }).click();

  const modal = page.locator('[data-modal="node-editor"]');
  const ipv4 = modal.getByLabel('IPv4 監聽地址');
  const ipv6 = modal.getByLabel('IPv6 監聽地址');
  const fieldError = modal.locator('[data-node-listener-error]');
  await modal.getByLabel('節點 ID').fill('edge-ui');
  await modal.getByRole('button', { name: '確認並套用' }).click();

  await expect(fieldError).toBeVisible();
  await expect(fieldError).toContainText('至少需要一個 IPv4 或 IPv6 監聽地址');
  await expect(ipv4).toHaveAttribute('aria-invalid', 'true');
  await expect(ipv6).toHaveAttribute('aria-invalid', 'true');
  await expect(ipv4).toHaveAttribute('aria-describedby', /node-listener-error/);
  await expect(ipv6).toHaveAttribute('aria-describedby', /node-listener-error/);
  await expect(ipv4).toBeFocused();

  await ipv6.fill('2001:db8::10');
  await expect(fieldError).toBeHidden();
  await expect(ipv4).not.toHaveAttribute('aria-invalid', 'true');
  await expect(ipv6).not.toHaveAttribute('aria-invalid', 'true');
});

test('版面無水平溢出或主要導覽重疊且不載入外部資產', async ({ page }) => {
  const externalRequests: string[] = [];
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.hostname !== '127.0.0.1') {
      externalRequests.push(request.url());
    }
  });
  await login(page);
  await page.waitForLoadState('networkidle');

  const layout = await page.evaluate(() => {
    const buttons = [...document.querySelectorAll<HTMLElement>('nav button')];
    const boxes = buttons.map((button) => button.getBoundingClientRect());
    let overlap = false;
    for (let left = 0; left < boxes.length; left += 1) {
      for (let right = left + 1; right < boxes.length; right += 1) {
        const a = boxes[left];
        const b = boxes[right];
        if (a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top) {
          overlap = true;
        }
      }
    }
    const overflowingButtons = [...document.querySelectorAll<HTMLElement>('button')]
      .filter((button) => button.offsetParent !== null && button.scrollWidth > button.clientWidth + 1)
      .map((button) => button.textContent?.trim() ?? '');
    return {
      horizontalOverflow: document.documentElement.scrollWidth > window.innerWidth + 1,
      overlap,
      overflowingButtons,
    };
  });
  expect(layout).toEqual({ horizontalOverflow: false, overlap: false, overflowingButtons: [] });
  expect(externalRequests).toEqual([]);
});

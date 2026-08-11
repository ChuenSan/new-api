# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: model-route-qa.spec.js >> model route policies support grouped reorder, filter safety, and editable top priority
- Location: ../../../../../../../private/tmp/model-route-qa.spec.js:3:5

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator: getByRole('heading', { name: 'gpt-test' })
Expected: visible
Timeout: 5000ms
Error: element(s) not found

Call log:
  - Expect "toBeVisible" with timeout 5000ms
  - waiting for getByRole('heading', { name: 'gpt-test' })

```

```yaml
- heading "500" [level=1]
- text: Oops! Something went wrong :')
- paragraph: We apologize for the inconvenience. Please try again later.
- paragraph: If this keeps happening, please report it on GitHub Issues.
- button "Go Back"
- button "Report an issue"
- button "Back to Home"
```

# Test source

```ts
  1   | import { test, expect } from '/private/var/folders/k2/nzscf2f51yb6xc67ndc9bxgr0000gn/T/bunx-501-playwright@latest/node_modules/playwright/test.mjs'
  2   |
  3   | test('model route policies support grouped reorder, filter safety, and editable top priority', async ({ page }) => {
  4   |   const consoleIssues = []
  5   |   page.on('console', (message) => {
  6   |     if (message.type() === 'error' || message.type() === 'warning') {
  7   |       consoleIssues.push(`${message.type()}: ${message.text()}`)
  8   |     }
  9   |   })
  10  |
  11  |   const user = { id: 1, username: 'root', display_name: 'Root', role: 100, status: 1 }
  12  |   let policies = [
  13  |     { channel_id: 1, channel_name: 'Alpha', requested_model: 'gpt-test', effective_model: 'gpt-test', manual_priority: 100, enabled: true, source: 'configured' },
  14  |     { channel_id: 2, channel_name: 'Beta', requested_model: 'gpt-test', effective_model: 'provider/gpt-test', manual_priority: 90, enabled: true, source: 'mapped' },
  15  |     { channel_id: 3, channel_name: 'Claude', requested_model: 'claude-test', effective_model: 'claude-test', manual_priority: 50, enabled: true, source: 'configured' },
  16  |   ]
  17  |   let reorderRequests = 0
  18  |   let priorityRequests = 0
  19  |
  20  |   await page.addInitScript((savedUser) => {
  21  |     localStorage.setItem('user', JSON.stringify(savedUser))
  22  |   }, user)
  23  |
  24  |   await page.route('**/api/**', async (route) => {
  25  |     const request = route.request()
  26  |     const pathname = new URL(request.url()).pathname
  27  |     if (pathname === '/api/setup') {
  28  |       await route.fulfill({ json: { success: true, message: '', data: { status: true } } })
  29  |       return
  30  |     }
  31  |     if (pathname === '/api/user/self') {
  32  |       await route.fulfill({ json: { success: true, message: '', data: user } })
  33  |       return
  34  |     }
  35  |     if (pathname === '/api/model_route/policies' && request.method() === 'GET') {
  36  |       await route.fulfill({ json: { success: true, message: '', data: policies } })
  37  |       return
  38  |     }
  39  |     if (pathname === '/api/model_route/metrics') {
  40  |       await route.fulfill({ json: { success: true, message: '', data: [] } })
  41  |       return
  42  |     }
  43  |     if (pathname === '/api/model_route/policies/reorder') {
  44  |       reorderRequests++
  45  |       const body = request.postDataJSON()
  46  |       const group = policies.filter((policy) => policy.requested_model === body.requested_model)
  47  |       const byID = new Map(group.map((policy) => [policy.channel_id, policy]))
  48  |       const ordered = body.ordered_channel_ids.map((id) => byID.get(id))
  49  |       ordered[0] = { ...ordered[0], manual_priority: 101 }
  50  |       ordered[1] = { ...ordered[1], manual_priority: 100 }
  51  |       policies = [...ordered, ...policies.filter((policy) => policy.requested_model !== body.requested_model)]
  52  |       await route.fulfill({
  53  |         json: {
  54  |           success: true,
  55  |           message: '',
  56  |           data: {
  57  |             requested_model: body.requested_model,
  58  |             changed: ordered.map((policy) => ({ channel_id: policy.channel_id, manual_priority: policy.manual_priority })),
  59  |             policies: ordered,
  60  |           },
  61  |         },
  62  |       })
  63  |       return
  64  |     }
  65  |     if (pathname === '/api/model_route/policies/priority') {
  66  |       priorityRequests++
  67  |       const body = request.postDataJSON()
  68  |       policies = policies.map((policy) =>
  69  |         policy.channel_id === body.channel_id && policy.requested_model === body.requested_model
  70  |           ? { ...policy, manual_priority: body.manual_priority }
  71  |           : policy
  72  |       )
  73  |       const group = policies
  74  |         .filter((policy) => policy.requested_model === body.requested_model)
  75  |         .sort((left, right) => right.manual_priority - left.manual_priority)
  76  |       await route.fulfill({
  77  |         json: {
  78  |           success: true,
  79  |           message: '',
  80  |           data: {
  81  |             requested_model: body.requested_model,
  82  |             changed: [{ channel_id: body.channel_id, manual_priority: body.manual_priority }],
  83  |             policies: group,
  84  |           },
  85  |         },
  86  |       })
  87  |       return
  88  |     }
  89  |     await route.fulfill({ json: { success: true, message: '', data: {} } })
  90  |   })
  91  |
  92  |   await page.goto('http://127.0.0.1:3000/model-route/')
  93  |   await expect(page).toHaveURL(/\/model-route\/$/)
> 94  |   await expect(page.getByRole('heading', { name: 'gpt-test' })).toBeVisible()
      |                                                                 ^ Error: expect(locator).toBeVisible() failed
  95  |   await expect(page.getByRole('heading', { name: 'claude-test' })).toBeVisible()
  96  |   await expect(page.locator('body')).not.toContainText('Runtime Error')
  97  |
  98  |   const betaRow = page.getByRole('row').filter({ hasText: 'Beta' })
  99  |   const betaHandle = betaRow.getByRole('button', { name: 'Drag to reorder' })
  100 |   await betaHandle.focus()
  101 |   await betaHandle.press('Space')
  102 |   await betaHandle.press('ArrowUp')
  103 |   await betaHandle.press('Space')
  104 |   await expect.poll(() => reorderRequests).toBe(1)
  105 |   await expect(page.locator('section').filter({ hasText: 'gpt-test' }).getByRole('row').nth(1)).toContainText('Beta')
  106 |
  107 |   const channelFilter = page.getByPlaceholder('Channel name or ID')
  108 |   await channelFilter.fill('Alpha')
  109 |   await expect(page.getByText('Clear the channel filter to reorder the complete model group.')).toBeVisible()
  110 |   await expect(page.getByRole('row').filter({ hasText: 'Beta' })).toHaveCount(0)
  111 |   await expect(page.getByRole('row').filter({ hasText: 'Alpha' }).getByRole('button', { name: 'Drag to reorder' })).toBeDisabled()
  112 |   await channelFilter.clear()
  113 |
  114 |   const alphaRow = page.getByRole('row').filter({ hasText: 'Alpha' })
  115 |   await alphaRow.getByRole('button', { name: 'Set as first' }).click()
  116 |   const suggestedPriority = page.getByRole('spinbutton')
  117 |   await expect(suggestedPriority).toHaveValue('201')
  118 |   await suggestedPriority.fill('250')
  119 |   await page.getByRole('button', { name: 'Apply' }).click()
  120 |   await expect.poll(() => priorityRequests).toBe(1)
  121 |
  122 |   await page.screenshot({ path: '/tmp/model-route-desktop.png', fullPage: false })
  123 |   await page.setViewportSize({ width: 390, height: 844 })
  124 |   await page.screenshot({ path: '/tmp/model-route-mobile.png', fullPage: false })
  125 |   expect(consoleIssues).toEqual([])
  126 | })
  127 |
```

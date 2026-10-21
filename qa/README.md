# QA

## Playwright

```bash
cd qa
npm install
npx playwright install --with-deps chromium
npm run test:e2e
```

Run against a test instance configured with an initialized database. The current smoke test checks the setup status and login page. Add authenticated CRUD tests with a disposable test database before production.

## k6

```bash
k6 run qa/k6-smoke.js
```

Run only against local or staging systems where load testing is authorized.

## Lighthouse CI

Install Lighthouse CI, start the app, then run:

```bash
npm install -g @lhci/cli
lhci autorun
```

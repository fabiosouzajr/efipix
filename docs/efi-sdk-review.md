# EFí Go SDK Capability Review (Phase 1)

SDK: `github.com/efipay/sdk-go-apis-efi` v1.4.0 (source inspected directly under
`$(go env GOPATH)/pkg/mod/github.com/efipay/sdk-go-apis-efi@v1.4.0`).

## Confirmed for Phase 1 (immediate charge)

- Client constructor: `pix.NewEfiPay(configs map[string]interface{}) *pix` in
  `src/efipay/pix/efiPay_pix.go`. The returned type is an unexported `*pix`,
  so callers must capture it behind a local interface (see `efiSDKClient` in
  `internal/provider/efi/sdkclient.go`) rather than naming the concrete type.
- Required `configs` keys (all read via unchecked type assertion -> a missing
  or mistyped key panics, not errors):
  - `client_id` (string)
  - `client_secret` (string)
  - `CA` (string) - **file path** to the client certificate PEM, passed to
    `tls.LoadX509KeyPair(CA, Key)`. NOT raw PEM bytes, and NOT a single
    combined `certificate` field as the plan template guessed.
  - `Key` (string) - file path to the private key PEM, same `LoadX509KeyPair`
    call.
  - `sandbox` (bool)
  - `timeout` (int) - HTTP client timeout in seconds.
  - `SDKFactory` writes `creds.CertPEM`/`creds.KeyPEM` to two separate temp
    files and passes those paths as `CA`/`Key`.
- Create immediate charge with client-defined txid: `CreateCharge(txid string,
  body map[string]interface{}) (string, error)` -> `PUT /v2/cob/:txid`
  (idempotent). This is the method to use, **not** `PixCreateCharge` /
  `PixCreateImmediateCharge` (those exist but are for the
  server-generates-txid `POST /v2/cob` flow, which Phase 1 doesn't use).
- Detail/get charge: `DetailCharge(txid string) (string, error)` ->
  `GET /v2/cob/:txid`. **Not** `PixDetailCharge` (no such method exists).
- All endpoint methods return `(string, error)` where the string is the raw
  JSON response body, and the error is `errors.New(rawBody)` for any non-200/
  201 HTTP status (auth retried once automatically on failure).
- Certificate is bound at client construction (per-instance) -> we pool one
  `efiSDKClient` per `payment_provider_id`, as already implemented in
  `EfiProvider.clientFor`.
- Response fields observed on the cob resource (`/v2/cob` and `/v2/cob/:txid`):
  `txid`, `status`, `loc.id` (numeric), `pixCopiaECola` (the "copia e cola"
  BR Code payload). These are parsed by `parseCob` in `sdkclient.go`.

## QR code image

- The cob resource itself does **not** include a QR code image field.
- The base64 PNG (`imagemQrcode`) is only available via a separate call:
  `PixGenerateQRCode(id string) (string, error)` -> `GET /v2/loc/:id/qrcode`,
  where `id` is the numeric `loc.id` from the cob resource.
- `sdkClient.CreateCob` makes this second call best-effort after creating the
  charge, to populate `cobOutput.QRCodeImage`. If it fails, charge creation
  still succeeds with an empty `QRCodeImage` (the txid/payload are already
  valid).
- `sdkClient.GetCob` does **not** re-fetch the QR image (only status/payload
  are needed for polling). If a later phase needs to redisplay the QR code on
  a "get charge" call, add the same `PixGenerateQRCode` lookup there.

## Gaps / notes for later phases

- `cobv` (due charges), `devolução` (refunds), webhook config, and split
  config all have endpoint methods in `src/efipay/pix/endpoints_pix.go`
  (`CreateDueCharge`, `DetailDueCharge`, `PixDevolution`,
  `PixConfigWebhook`, `PixSplitConfig`, etc.) but were not exercised here -
  confirm body/response shapes when those phases are implemented.
- No homologation credentials were available during this review; the SDK
  surface above is confirmed from source code (method signatures, config key
  names, response-string contract) but not from a live sandbox call. The
  `sdkclient_homolog_test.go` (build tag `homolog`) exercises the real flow
  end-to-end once `EFI_CREDENTIALS`, `EFI_TEST_PROVIDER_ID`, and
  `EFI_TEST_PIX_KEY` are set.

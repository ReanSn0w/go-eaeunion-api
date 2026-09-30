# Go-клиент REST API ЕАЭС

Пакет `eaeunion` читает любую доступную REST-коллекцию через одну настроенную точку `/find`. Он не хранит каталог коллекций и не навязывает модели конкретных реестров. Требуется Go 1.23+; внешних зависимостей нет.

Для Единого реестра органов по оценке соответствия есть [типизированная обёртка `conformity`](conformity/README.md).

```go
client, err := eaeunion.NewClient(eaeunion.Config{
    Endpoint: "https://tech.eaeunion.org/spd/find",
})
if err != nil { return err }

limit := 10
page, err := client.Find(ctx,
    "kbdread.service-prop-36-v_conformityAssessmentBodyInformationDetailsType_organization_table",
    eaeunion.Query{
        Filter: map[string]any{"unifiedCountryCode.value": "RU"},
        Limit: &limit,
        Sort: []eaeunion.SortField{{Name: "conformityAuthorityId", Direction: 1}},
        Fields: []eaeunion.Field{{Name: "conformityAuthorityId", Include: 1}},
    },
)
if err != nil { return err }
for _, raw := range page.Result { fmt.Println(string(raw)) }
```

`Filter` принимает произвольный JSON-объект или `json.RawMessage`. Имена и значения полей зависят от коллекции. `nil` в `Limit`, `Skip`, `SkipCount` означает, что параметр не передаётся; `limit=0` запрещён, поскольку сервер фактически трактует его как запрос всей коллекции. Сортировка сериализуется в порядке элементов списка. `DecodePage[T](page)` декодирует записи в пользовательский тип, сохраняя большие числа в полях `any` как `json.Number`.

Для последовательной обработки используйте `Walk`. Он по умолчанию запрашивает не более 500 записей на страницу, вызывает callback для страницы и возвращает `NextSkip` после последней принятой страницы. Возврат `false` останавливает обход до принятия текущей страницы. `MaxPages` и `MaxRecords` ограничивают выгрузку; при ошибке уже пройденные страницы остаются учтёнными в результате. Неизменяемость выборки сервер не гарантирует: для воспроизводимого обхода задавайте устойчивую сортировку с уникальным дополнительным полем.

```go
result, err := client.Walk(ctx, collection,
    eaeunion.Query{Sort: []eaeunion.SortField{{Name: "_id", Direction: 1}}},
    eaeunion.WalkOptions{PageSize: 100, MaxRecords: 1000},
    func(page eaeunion.Page) (bool, error) {
        for _, raw := range page.Result { /* обработать raw */ _ = raw }
        return true, nil
    },
)
fmt.Println(result.NextSkip, err)
```

Для OAuth Client Credentials задайте `Mode: eaeunion.DirectToken` и `Credentials` с `TokenURL`, `ClientID`, `ClientSecret` либо передайте собственный `TokenProvider`, например `eaeunion.StaticToken(token)`. Для расширенного доступа задайте `Mode: eaeunion.Gateway` и `Gateway` с URL шлюза, `ServiceKey` и `CountryTo`. Для сервиса `/spd/find` ключ равен `spd`, путь во вложенном запросе выводится как `/find`. Токен OAuth кэшируется по `expires_in`; токен групповых политик обновляется при смене OAuth-токена или вложенном HTTP 401, поскольку срок его действия в справке не указан. Повтор чтения после 401 выполняется один раз. Получение прав и реквизитов происходит вне библиотеки; берите их из окружения или секретного хранилища, не записывайте в исходный код. Обычное прямое авторизованное чтение и шлюз не проверены вживую без учётных данных.

Имена коллекций ищите в REST-вкладках [карточек ресурсов](https://opendata.eaeunion.org/opendata/ru/resourses) и тематических API-разделах. Глобального документированного метода перечисления нет. Например, [API госзакупок](https://goszakupki.eaeunion.org/erpt/ru/registers/api) публикует `erpt.v_goodscollection_prod_public`, `erpt.v_RegisterOfWithdrawalNotifications_public`, `erpt.v_databasesection_public` и `erpt.v_RegisterOfManufacturers_publish` для `https://goszakupki.eaeunion.org/spd/find`. Доступность одной коллекции не означает доступность других на том же сервере.

Ошибки внешнего HTTP доступны как `*eaeunion.HTTPError`, внутренние ошибки шлюза — как `*eaeunion.GatewayError`, превышение лимита ответа — `eaeunion.ErrResponseTooLarge`. Автоматических повторов HTTP 500 нет. Ответы без счётчиков допустимы, в частности при `skipCount=true`. Проверенный контракт и расхождения со справкой описаны в [docs/api-contract.md](docs/api-contract.md).

Обычные проверки не требуют сети: `go test ./...`, `go test -race ./...`, `go vet ./...`. Живую минимальную проверку запускайте явно:

```sh
EAEUNION_INTEGRATION=1 \
EAEUNION_ENDPOINT=https://tech.eaeunion.org/spd/find \
EAEUNION_COLLECTION=kbdread.service-prop-36-v_conformityAssessmentBodyInformationDetailsType_organization_table \
go test -run '^TestLiveCollection$' ./...
```

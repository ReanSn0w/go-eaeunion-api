# Реестр органов по оценке соответствия

Пакет `conformity` добавляет типы и проверенные фильтры для [реестра №36](https://tech.eaeunion.org/tech/ru/registers/36). По умолчанию используется публичная коллекция; для другой конфигурации передайте общий `*eaeunion.Client` в `conformity.NewClient`.

```go
client, err := conformity.NewPublicClient()
if err != nil { return err }

record, err := client.GetByID(ctx, "BY579", "BY")
if err != nil { return err }
fmt.Println(record.AuthorityID, record.Body.Name, record.Certificate.ID)
```

`GetByID` выдаёт `ErrNotFound` при пустом результате и `ErrAmbiguous` при нескольких совпадениях. Код страны можно оставить пустым, но уникальность идентификатора без страны не предполагается.

```go
limit := 20
page, err := client.Find(ctx, conformity.Filter{
    Country: "RU",
    NameContains: "лаборатория",
    RecordStatus: "01",
    AccreditationStatus: "02",
}, eaeunion.Query{Limit: &limit})
if err != nil { return err }
for _, item := range page.Records { fmt.Println(item.AuthorityID) }
```

`NameContains` экранирует спецсимволы регулярных выражений; `NameRegex` принимает выражение без экранирования. Дополнительные условия задаются через `Filter.Extra` либо `Query.Filter` и соединяются с именованными условиями оператором `$and`. `Record.Raw` содержит исходный JSON записи для ещё не описанных полей. Даты аттестата сохранены строками в `$date`, коды и статусы — строками. Смысл числовых кодов API здесь не угадывается.

Для обхода используйте `client.Walk(ctx, filter, query, options, callback)`: callback получает типизированную страницу, а результат содержит следующий offset. Подробности ограничений offset-пагинации описаны в [README общего клиента](../README.md).

Локально: `go test ./...`, `go test -race ./...`, `go vet ./...`. Минимальная живая проверка публичного реестра: `EAEUNION_INTEGRATION=1 go test -run '^TestLiveRegistry$' ./conformity`. Она не зависит от текущего числа записей или порядка строк. Наблюдавшаяся схема и её ограничения описаны в [docs/conformity-contract.md](../docs/conformity-contract.md).

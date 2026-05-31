# clean-fixture

A small example library used to verify that agentguard does not produce
false positives on legitimate, well-formed package documentation. Every
sentence in this README is the kind of phrasing a careful human
maintainer would write for other humans, and none of it is addressed to
an automated reader.

## Installation

```
npm install clean-fixture
```

## Usage

```js
const cf = require('clean-fixture');
console.log(cf.hello());  // → "hello, world"
```

The library exposes a single `hello` function so the surface stays small
enough that downstream projects can audit it end-to-end in a minute.

## Contributing

Please run the test suite (`npm test`) before opening a pull request.
Pull requests that include a regression test for the change are merged
faster. New contributors are encouraged to start with a small docs fix
so the review loop stays short on both sides.

If you would like to translate this README into another language, open
an issue describing the target language and link to a draft so we can
coordinate before the work begins.

## License

MIT. See the `LICENSE` file at the repository root for the full text.

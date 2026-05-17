import {useState, type ReactNode} from 'react';
import useBaseUrl from '@docusaurus/useBaseUrl';
import Layout from '@theme/Layout';

import './landing.css';

type InstallTab = 'brew' | 'brew1' | 'src' | 'run';

const INSTALL_TABS: Array<{id: InstallTab; label: string; copy: string; code: ReactNode}> = [
  {
    id: 'brew',
    label: 'Homebrew',
    copy: 'brew tap mekedron/tap\nbrew install wolt-cli',
    code: (
      <>
        <span className="tk-mut"># Add the tap, then install</span>
        {'\n'}
        <span className="tk-fn">brew</span> tap mekedron/tap{'\n'}
        <span className="tk-fn">brew</span> install wolt-cli
      </>
    ),
  },
  {
    id: 'brew1',
    label: 'One-liner',
    copy: 'brew install mekedron/tap/wolt-cli',
    code: (
      <>
        <span className="tk-mut"># Single command — tap is added implicitly</span>
        {'\n'}
        <span className="tk-fn">brew</span> install mekedron/tap/wolt-cli
      </>
    ),
  },
  {
    id: 'src',
    label: 'From source',
    copy:
      'git clone https://github.com/mekedron/wolt-cli.git\ncd wolt-cli\ngo build -o bin/wolt ./cmd/wolt\n./bin/wolt --help',
    code: (
      <>
        <span className="tk-mut"># Requires Go 1.26+</span>
        {'\n'}
        <span className="tk-fn">git</span> clone https://github.com/mekedron/wolt-cli.git{'\n'}
        <span className="tk-fn">cd</span> wolt-cli{'\n'}
        <span className="tk-fn">go</span> build -o bin/wolt ./cmd/wolt{'\n'}
        ./bin/wolt --help
      </>
    ),
  },
  {
    id: 'run',
    label: 'Without install',
    copy: 'go run ./cmd/wolt --help',
    code: (
      <>
        <span className="tk-mut"># No checkout, no install — just run</span>
        {'\n'}
        <span className="tk-fn">go</span> run ./cmd/wolt --help
      </>
    ),
  },
];

function useCopy() {
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const copy = (key: string, text: string) => {
    const finish = () => {
      setCopiedKey(key);
      window.setTimeout(() => {
        setCopiedKey((current) => (current === key ? null : current));
      }, 1400);
    };
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(text).then(finish, finish);
    } else {
      finish();
    }
  };
  return {copiedKey, copy};
}

function InlineCmd({
  id,
  text,
  size,
  children,
}: {
  id: string;
  text: string;
  size?: 'lg';
  children: ReactNode;
}) {
  const {copiedKey, copy} = useCopy();
  const isCopied = copiedKey === id;
  return (
    <div className={`cmd${size === 'lg' ? ' cmd--lg' : ''}${isCopied ? ' is-copied' : ''}`}>
      <span className="cmd__prompt">$</span>
      <span className="cmd__text">{children}</span>
      <button
        type="button"
        className="cmd__copy"
        aria-label="Copy install command"
        onClick={() => copy(id, text)}>
        <svg viewBox="0 0 24 24" width={size === 'lg' ? 18 : 16} height={size === 'lg' ? 18 : 16} aria-hidden="true">
          <path
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M8 8V5a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2h-3M5 8h9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-9a2 2 0 0 1 2-2Z"
          />
        </svg>
        <span className="cmd__copied">copied</span>
      </button>
    </div>
  );
}

function InstallTabs() {
  const [active, setActive] = useState<InstallTab>('brew');
  const {copiedKey, copy} = useCopy();
  return (
    <div className="tabs">
      <div className="tabs__bar" role="tablist">
        {INSTALL_TABS.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            className={`tabs__tab${active === tab.id ? ' is-active' : ''}`}
            aria-selected={active === tab.id}
            onClick={() => setActive(tab.id)}>
            {tab.label}
          </button>
        ))}
      </div>
      <div className="tabs__panels">
        {INSTALL_TABS.map((tab) => {
          const isCopied = copiedKey === `tab-${tab.id}`;
          return (
            <div
              key={tab.id}
              className={`tabs__panel${active === tab.id ? ' is-active' : ''}`}>
              <div className="codeblock">
                <pre>
                  <code>{tab.code}</code>
                </pre>
                <button
                  type="button"
                  className={`codeblock__copy${isCopied ? ' is-copied' : ''}`}
                  onClick={() => copy(`tab-${tab.id}`, tab.copy)}>
                  {isCopied ? 'Copied' : 'Copy'}
                </button>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function Hero() {
  return (
    <section className="hero">
      <div className="hero__grid">
        <div className="hero__copy">
          <span className="eyebrow">
            <span className="eyebrow__dot" />
            v0 · Community Go CLI · MIT
          </span>
          <h1 className="hero__title">
            Browse, search, and cart
            <br />
            <span className="grad">without leaving the terminal.</span>
          </h1>
          <p className="hero__lede">
            <strong>wolt-cli</strong> is an unofficial, community-built Go CLI for
            interacting with Wolt endpoints. Discovery feed, venue search, menus,
            option matrices, carts, checkout preview — straight from your shell.
          </p>

          <div className="install-row" aria-label="Quick install">
            <InlineCmd id="hero-cmd" text="brew install mekedron/tap/wolt-cli">
              <span className="tk-fn">brew</span> install mekedron/tap/wolt-cli
            </InlineCmd>
          </div>

          <div className="hero__ctas">
            <a className="btn btn--primary btn--lg" href="#install">
              Get started
            </a>
            <a className="btn btn--ghost btn--lg" href="#example">
              See real example →
            </a>
          </div>

          <ul className="meta-strip" aria-label="Project facts">
            <li>
              <span className="meta-strip__k">Lang</span>
              <span className="meta-strip__v">Go 1.26+</span>
            </li>
            <li>
              <span className="meta-strip__k">Install</span>
              <span className="meta-strip__v">Homebrew / source</span>
            </li>
            <li>
              <span className="meta-strip__k">Output</span>
              <span className="meta-strip__v">table · json · yaml</span>
            </li>
            <li>
              <span className="meta-strip__k">License</span>
              <span className="meta-strip__v">MIT</span>
            </li>
          </ul>
        </div>

        <div className="terminal" role="img" aria-label="Terminal demo of wolt-cli">
          <div className="terminal__chrome">
            <span className="dot dot--r" />
            <span className="dot dot--y" />
            <span className="dot dot--g" />
            <span className="terminal__title">~/projects — zsh — 92×24</span>
          </div>
          <pre className="terminal__body">
            <code>
              <span className="t-mut">$</span> <span className="t-fn">wolt</span> search venues --query "burger king" --limit 5{'\n'}
              <span className="t-mut">┌─────────────────────────────────┬──────────────────────────┬──────────┐</span>{'\n'}
              <span className="t-mut">│</span> <span className="t-hd">SLUG</span>                            <span className="t-mut">│</span> <span className="t-hd">NAME</span>                     <span className="t-mut">│</span> <span className="t-hd">ETA</span>      <span className="t-mut">│</span>{'\n'}
              <span className="t-mut">├─────────────────────────────────┼──────────────────────────┼──────────┤</span>{'\n'}
              <span className="t-mut">│</span> burger-king-finnoo              <span className="t-mut">│</span> Burger King · Finnoo     <span className="t-mut">│</span> 20–30 m  <span className="t-mut">│</span>{'\n'}
              <span className="t-mut">│</span> burger-king-kamppi              <span className="t-mut">│</span> Burger King · Kamppi     <span className="t-mut">│</span> 15–25 m  <span className="t-mut">│</span>{'\n'}
              <span className="t-mut">│</span> burger-king-tapiola             <span className="t-mut">│</span> Burger King · Tapiola    <span className="t-mut">│</span> 25–35 m  <span className="t-mut">│</span>{'\n'}
              <span className="t-mut">└─────────────────────────────────┴──────────────────────────┴──────────┘</span>{'\n'}
              {'\n'}
              <span className="t-mut">$</span> <span className="t-fn">wolt</span> venue menu burger-king-finnoo --include-options <span className="t-fl">--format</span> json \{'\n'}
              {'  '}| jq -r '.data.items[] | select(.name|test("whopper";"i"))'{'\n'}
              {'\n'}
              <span className="t-mut">$</span> <span className="t-fn">wolt</span> cart add 629f...25f0 6769...cc6f \{'\n'}
              {'    '}<span className="t-fl">--venue-slug</span> burger-king-finnoo \{'\n'}
              {'    '}<span className="t-fl">--option</span> "drink=zero" <span className="t-fl">--option</span> "side=fries-l"{'\n'}
              <span className="t-ok">✓</span> added 1× <span className="t-st">WHOPPER Meal</span>   subtotal <span className="t-st">€11.95</span>{'\n'}
              {'\n'}
              <span className="t-mut">$</span> <span className="t-fn">wolt</span> checkout preview <span className="t-fl">--venue-id</span> 629f...25f0{'\n'}
              {'  '}items                           <span className="t-st">€11.95</span>{'\n'}
              {'  '}delivery                         <span className="t-st">€2.90</span>{'\n'}
              {'  '}service                          <span className="t-st">€0.99</span>{'\n'}
              {'  '}──────────────────────────────{'\n'}
              {'  '}<span className="t-em">total</span>                          <span className="t-em">€15.84</span>{'\n'}
              <span className="t-mut">$</span> <span className="t-cursor">▋</span>
            </code>
          </pre>
        </div>
      </div>
    </section>
  );
}

function Trust() {
  const cells = [
    {big: '12+', lbl: 'command groups'},
    {big: '3', lbl: 'output formats'},
    {big: '1', lbl: 'binary, zero deps'},
    {big: '0', lbl: 'orders placed by CLI'},
  ];
  return (
    <section className="trust" aria-label="At a glance">
      <div className="trust__row">
        {cells.map((c) => (
          <div key={c.lbl} className="trust__cell">
            <span className="trust__big">{c.big}</span>
            <span className="trust__lbl">{c.lbl}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function Features() {
  const items: Array<{title: ReactNode; body: ReactNode; cmd: string; icon: ReactNode}> = [
    {
      icon: (
        <svg viewBox="0 0 24 24">
          <path
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M3 5h18M3 12h18M3 19h12"
          />
        </svg>
      ),
      title: <>Discovery &amp; search</>,
      body: 'List the discovery feed, browse categories, and search venues or items by query. Filter by locale, address, or coordinates.',
      cmd: 'wolt search venues --query "ramen"',
    },
    {
      icon: (
        <svg viewBox="0 0 24 24">
          <path
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M4 7h16M4 12h16M4 17h10"
          />
          <circle cx="20" cy="17" r="2" fill="currentColor" />
        </svg>
      ),
      title: <>Venue details &amp; menus</>,
      body: (
        <>
          Inspect a venue's hours, menu, categories, and item details. Stream menus
          with <code>--include-options</code> for the full option matrix.
        </>
      ),
      cmd: 'wolt venue menu <slug> --include-options',
    },
    {
      icon: (
        <svg viewBox="0 0 24 24">
          <path
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M6 7h12l-1.4 9.2a2 2 0 0 1-2 1.8h-5.2a2 2 0 0 1-2-1.8L6 7Zm0 0L5 4H3"
          />
          <circle cx="10" cy="21" r="1.4" fill="currentColor" />
          <circle cx="16" cy="21" r="1.4" fill="currentColor" />
        </svg>
      ),
      title: <>Cart operations</>,
      body: (
        <>
          <code>show</code>, <code>count</code>, <code>add</code>, <code>remove</code>,{' '}
          <code>clear</code>. Add items with arbitrarily nested options the same way the
          app does.
        </>
      ),
      cmd: 'wolt cart add <venue> <item> --option …',
    },
    {
      icon: (
        <svg viewBox="0 0 24 24">
          <path
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M4 12h6l2-3 4 6 2-3h2"
          />
        </svg>
      ),
      title: 'Checkout preview',
      body: (
        <>
          Run <code>checkout preview</code> to project totals, fees, and delivery cost
          from your current cart — without ever placing an order.
        </>
      ),
      cmd: 'wolt checkout preview --venue-id …',
    },
    {
      icon: (
        <svg viewBox="0 0 24 24">
          <circle cx="12" cy="8" r="4" fill="none" stroke="currentColor" strokeWidth="1.6" />
          <path
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            d="M4 20c1.5-3.5 4.7-5.5 8-5.5s6.5 2 8 5.5"
          />
        </svg>
      ),
      title: <>Profile &amp; orders</>,
      body: 'Auth status, profile, past orders, saved addresses, payments, and favorites — read-only and respectful of your account scope.',
      cmd: 'wolt profile orders --limit 20',
    },
    {
      icon: (
        <svg viewBox="0 0 24 24">
          <path
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M5.6 18.4l2.1-2.1M16.3 7.7l2.1-2.1"
          />
          <circle cx="12" cy="12" r="4" fill="none" stroke="currentColor" strokeWidth="1.6" />
        </svg>
      ),
      title: 'Token rotation',
      body: (
        <>
          Auth via <code>--wtoken</code>, refresh with <code>--wrtoken</code>, or cookie
          pairs. Profiles isolate credentials per environment.
        </>
      ),
      cmd: 'wolt configure --profile-name default …',
    },
    {
      icon: (
        <svg viewBox="0 0 24 24">
          <path
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M3 5h18v3H3zM3 11h18v3H3zM3 17h12v3H3z"
          />
        </svg>
      ),
      title: 'Pipeable output',
      body: (
        <>
          Every command emits <code>table</code>, <code>json</code>, or <code>yaml</code>.
          Pipe straight into <code>jq</code>, <code>yq</code>, or your own scripts.
        </>
      ),
      cmd: "--format json | jq '.data.items[]'",
    },
    {
      icon: (
        <svg viewBox="0 0 24 24">
          <path
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M12 2C7 8 4 11 4 14a8 8 0 0 0 16 0c0-3-3-6-8-12Z"
          />
        </svg>
      ),
      title: 'Location override',
      body: (
        <>
          Pass <code>--address</code> or <code>--lat/--lon</code> per command.
          Preview-only — final orders still use your saved Wolt address.
        </>
      ),
      cmd: '--address "Mannerheimintie 1, Helsinki"',
    },
  ];

  return (
    <section id="features" className="features">
      <header className="section-head">
        <span className="section-head__eyebrow">What it covers</span>
        <h2 className="section-head__title">Every Wolt surface, in your shell.</h2>
        <p className="section-head__lede">
          From discovery to checkout preview — the same flows you'd click through in the
          app, scriptable and pipeable.
        </p>
      </header>

      <div className="features__grid">
        {items.map((it, i) => (
          <article key={i} className="feature">
            <div className="feature__icon" aria-hidden="true">
              {it.icon}
            </div>
            <h3>{it.title}</h3>
            <p>{it.body}</p>
            <code className="feature__cmd">{it.cmd}</code>
          </article>
        ))}
      </div>
    </section>
  );
}

function Install() {
  return (
    <section id="install" className="install">
      <header className="section-head">
        <span className="section-head__eyebrow">Install</span>
        <h2 className="section-head__title">One command. One binary.</h2>
        <p className="section-head__lede">
          Homebrew is the recommended path. Building from source is a single{' '}
          <code>go build</code>.
        </p>
      </header>

      <InstallTabs />

      <div className="install__after">
        <div className="install__step">
          <span className="install__num">1</span>
          <h4>Configure a profile</h4>
          <p>Save your token and refresh token to a named profile. Cookie auth is supported too.</p>
          <pre className="snippet">
            <code>
              <span className="tk-fn">wolt</span> configure <span className="tk-fl">--profile-name</span> default \{'\n'}
              {'  '}<span className="tk-fl">--wtoken</span> "&lt;token&gt;" <span className="tk-fl">--wrtoken</span> "&lt;refresh&gt;" <span className="tk-fl">--overwrite</span>
            </code>
          </pre>
        </div>
        <div className="install__step">
          <span className="install__num">2</span>
          <h4>Verify it works</h4>
          <p>Status checks the loaded profile and validates the token against the API.</p>
          <pre className="snippet">
            <code>
              <span className="tk-fn">wolt</span> profile status <span className="tk-fl">--verbose</span>{'\n'}
              <span className="tk-fn">wolt</span> profile show <span className="tk-fl">--format</span> json
            </code>
          </pre>
        </div>
        <div className="install__step">
          <span className="install__num">3</span>
          <h4>Run anything</h4>
          <p>Pipe into <code>jq</code>, save to disk, or feed another script. Output format is yours.</p>
          <pre className="snippet">
            <code>
              <span className="tk-fn">wolt</span> search venues <span className="tk-fl">--query</span> "ramen" <span className="tk-fl">--format</span> json \{'\n'}
              {'  '}| jq -r '.data.items[].slug'
            </code>
          </pre>
        </div>
      </div>
    </section>
  );
}

function Example() {
  return (
    <section id="example" className="example">
      <header className="section-head section-head--dark">
        <span className="section-head__eyebrow">Real example</span>
        <h2 className="section-head__title">Build a custom WHOPPER meal — end to end.</h2>
        <p className="section-head__lede">
          Five steps, all in your terminal. No order is placed; checkout is preview only.
        </p>
      </header>

      <ol className="steps">
        <li className="step">
          <header>
            <span className="step__n">01</span>
            <h3>Find the venue</h3>
          </header>
          <p>
            Search for the venue and copy its <code>slug</code> + <code>venue_id</code>.
          </p>
          <pre className="snippet snippet--dark">
            <code>
              <span className="tk-fn">wolt</span> search venues <span className="tk-fl">--query</span> "burger king" <span className="tk-fl">--limit</span> 10 <span className="tk-fl">--format</span> json \{'\n'}
              {'  '}| jq -r '.data.items[] | "\\(.slug)\\t\\(.venue_id)\\t\\(.name)"'
            </code>
          </pre>
        </li>
        <li className="step">
          <header>
            <span className="step__n">02</span>
            <h3>Inspect the menu</h3>
          </header>
          <p>Pull the menu with the option matrix and locate the item you want.</p>
          <pre className="snippet snippet--dark">
            <code>
              <span className="tk-fn">wolt</span> venue menu burger-king-finnoo <span className="tk-fl">--include-options</span> <span className="tk-fl">--format</span> json \{'\n'}
              {'  '}| jq -r '.data.items[] | select(.name|test("whopper";"i")){'\n'}
              {'                         '}| "\\(.item_id)\\t\\(.name)\\t\\(.base_price.amount)"'
            </code>
          </pre>
        </li>
        <li className="step">
          <header>
            <span className="step__n">03</span>
            <h3>Read the options</h3>
          </header>
          <p>Resolve drink, side, and add-on group IDs to value IDs.</p>
          <pre className="snippet snippet--dark">
            <code>
              <span className="tk-fn">wolt</span> item options burger-king-finnoo &lt;item-id&gt; <span className="tk-fl">--format</span> json \{'\n'}
              {'  '}| jq -r '.data.option_groups[] | .name as $g | .values[] | "\\($g)\\t\\(.name)"'
            </code>
          </pre>
        </li>
        <li className="step">
          <header>
            <span className="step__n">04</span>
            <h3>Add to cart with options</h3>
          </header>
          <p>
            Repeatable <code>--option group=value</code> flags compose the whole order.
          </p>
          <pre className="snippet snippet--dark">
            <code>
              <span className="tk-fn">wolt</span> cart add 629f1f18480882d6f02c25f0 676939cb70769df4cec6cc6f \{'\n'}
              {'  '}<span className="tk-fl">--venue-slug</span> burger-king-finnoo \{'\n'}
              {'  '}<span className="tk-fl">--option</span> "69958f7a0ccf540d98667a70=69958f777cb002552fad3d3d" \{'\n'}
              {'  '}<span className="tk-fl">--option</span> "6995b941621e894833915306=6995b93d45f708d8b1ad1345" \{'\n'}
              {'  '}<span className="tk-fl">--option</span> "69958f7a0ccf540d98667a73=69958f777cb002552fad3d51" \{'\n'}
              {'  '}<span className="tk-fl">--format</span> json
            </code>
          </pre>
        </li>
        <li className="step">
          <header>
            <span className="step__n">05</span>
            <h3>Preview the checkout</h3>
          </header>
          <p>Project items, fees, and totals — nothing is submitted.</p>
          <pre className="snippet snippet--dark">
            <code>
              <span className="tk-fn">wolt</span> cart show <span className="tk-fl">--details</span> <span className="tk-fl">--venue-id</span> &lt;venue-id&gt; <span className="tk-fl">--format</span> json{'\n'}
              <span className="tk-fn">wolt</span> checkout preview <span className="tk-fl">--delivery-mode</span> standard <span className="tk-fl">--venue-id</span> &lt;venue-id&gt;
            </code>
          </pre>
        </li>
      </ol>
    </section>
  );
}

function Commands() {
  const cards: Array<{name: string; body: ReactNode}> = [
    {name: 'wolt configure', body: 'Create or update profiles. Set tokens, cookies, default locale, output format.'},
    {name: 'wolt discovery', body: 'Browse the discovery feed and categories scoped to a location.'},
    {name: 'wolt search', body: 'Search venues and items by free-text query.'},
    {name: 'wolt venue', body: 'Details, hours, categories, menus, and per-venue search.'},
    {name: 'wolt item', body: 'Inspect a single item and its full option matrix.'},
    {
      name: 'wolt cart',
      body: (
        <>
          <code>show</code> · <code>count</code> · <code>add</code> · <code>remove</code> ·{' '}
          <code>clear</code>.
        </>
      ),
    },
    {name: 'wolt checkout preview', body: 'Project totals and fees from the current cart. No order placement.'},
    {name: 'wolt profile', body: 'Auth status, profile data, orders, addresses, payments, favorites.'},
  ];

  return (
    <section id="commands" className="commands">
      <header className="section-head">
        <span className="section-head__eyebrow">Command surface</span>
        <h2 className="section-head__title">Every group at a glance.</h2>
      </header>

      <div className="cmd-grid">
        {cards.map((c) => (
          <article key={c.name} className="cmd-card">
            <h4>
              <code>{c.name}</code>
            </h4>
            <p>{c.body}</p>
          </article>
        ))}
      </div>

      <details className="flags">
        <summary>Global flags reference</summary>
        <div className="flags__grid">
          <div><code>--format</code><span>table · json · yaml</span></div>
          <div><code>--profile</code><span>switch between configured profiles</span></div>
          <div><code>--address</code><span>temporary location, geocoded</span></div>
          <div><code>--lat / --lon</code><span>coordinate override (paired)</span></div>
          <div><code>--locale</code><span>BCP-47 locale tag</span></div>
          <div><code>--no-color</code><span>disable ANSI colors</span></div>
          <div><code>--verbose</code><span>HTTP trace + diagnostics</span></div>
          <div><code>--wtoken / --wrtoken</code><span>auth + refresh tokens</span></div>
          <div><code>--cookie</code><span>repeatable name=value pairs</span></div>
        </div>
      </details>
    </section>
  );
}

function FAQ() {
  const qas: Array<{q: string; a: ReactNode; open?: boolean}> = [
    {
      q: 'Is this an official Wolt product?',
      open: true,
      a: (
        <>
          No. wolt-cli is an <strong>unofficial, community-built</strong> Go CLI. It's
          not affiliated with, endorsed by, or supported by Wolt. Use it at your own
          responsibility, and respect their terms of service.
        </>
      ),
    },
    {
      q: 'Can it place real orders?',
      a: (
        <>
          No. The CLI exposes <code>checkout preview</code> only, which projects totals
          and fees. Final order placement still happens in the official Wolt app or
          website, using the delivery address selected in your account.
        </>
      ),
    },
    {
      q: 'Where is my config stored?',
      a: (
        <>
          By default at <code>~/.wolt/.wolt-config.json</code>, or wherever{' '}
          <code>WOLT_CONFIG_PATH</code> points. The file may contain <code>wtoken</code>
          , <code>wrtoken</code>, and cookies — keep it local and don't commit it. The
          project's <code>.gitignore</code> already ignores common config patterns.
        </>
      ),
    },
    {
      q: 'What about location overrides?',
      a: (
        <>
          Pass <code>--address</code> or <code>--lat/--lon</code> per command. They
          affect preview inputs only. Final orders still use your Wolt-saved address.{' '}
          <code>--lat</code> and <code>--lon</code> must be supplied together;{' '}
          <code>--address</code> can't be combined with them.
        </>
      ),
    },
    {
      q: 'Which platforms are supported?',
      a: 'Anywhere Go 1.26+ builds: macOS, Linux, and Windows. Homebrew is the smoothest path on macOS and Linux.',
    },
    {
      q: 'How do I report a bug or contribute?',
      a: (
        <>
          Open an issue or PR on the GitHub repository. Run <code>go test ./...</code>{' '}
          and <code>make lint</code> before submitting.
        </>
      ),
    },
  ];

  return (
    <section id="faq" className="faq">
      <header className="section-head">
        <span className="section-head__eyebrow">Honest answers</span>
        <h2 className="section-head__title">FAQ</h2>
      </header>

      <div className="faq__list">
        {qas.map((qa) => (
          <details key={qa.q} className="qa" open={qa.open}>
            <summary>{qa.q}</summary>
            <p>{qa.a}</p>
          </details>
        ))}
      </div>
    </section>
  );
}

function CTA() {
  return (
    <section className="cta">
      <div className="cta__inner">
        <h2>Try it in 30 seconds.</h2>
        <p>
          One <code>brew install</code>, one <code>wolt configure</code>, and you're
          piping menus into <code>jq</code>.
        </p>
        <InlineCmd id="cta-cmd" size="lg" text="brew install mekedron/tap/wolt-cli">
          <span className="tk-fn">brew</span> install mekedron/tap/wolt-cli
        </InlineCmd>
        <a
          className="btn btn--primary btn--lg"
          href="https://github.com/mekedron/wolt-cli"
          target="_blank"
          rel="noopener noreferrer">
          View on GitHub →
        </a>
      </div>
    </section>
  );
}

function Support() {
  return (
    <section id="support" className="support" aria-labelledby="support-h">
      <div className="support__inner">
        <div className="support__copy">
          <span className="support__badge">
            <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
              <path
                fill="currentColor"
                d="M12 21s-7-4.5-7-10a4.5 4.5 0 0 1 8-2.8A4.5 4.5 0 0 1 19 11c0 5.5-7 10-7 10Z"
              />
            </svg>
            Community-built
          </span>
          <h2 id="support-h" className="support__title">
            If wolt-cli saved you a tab, buy me a coffee.
          </h2>
          <p className="support__lede">
            wolt-cli is built and maintained by one developer in their free time — MIT
            licensed, free forever, no telemetry, no upsells. If it makes your terminal
            a little better, a small tip keeps it caffeinated.
          </p>
          <div className="support__ctas">
            <a
              className="btn btn--coffee btn--lg"
              href="https://buymeacoffee.com/mekedron"
              target="_blank"
              rel="noopener noreferrer">
              <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
                <path
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.8"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M5 10h12v5a4 4 0 0 1-4 4H9a4 4 0 0 1-4-4v-5Zm12 1h2a2.5 2.5 0 0 1 0 5h-2M8 3c0 1 1 1 1 2s-1 1-1 2M12 3c0 1 1 1 1 2s-1 1-1 2"
                />
              </svg>
              Buy me a coffee
            </a>
            <a
              className="btn btn--ghost btn--lg btn--on-warm"
              href="https://github.com/mekedron/wolt-cli"
              target="_blank"
              rel="noopener noreferrer">
              <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
                <path
                  fill="currentColor"
                  d="M12 .5A11.5 11.5 0 0 0 .5 12a11.5 11.5 0 0 0 7.86 10.92c.57.1.78-.25.78-.55v-2.1c-3.2.7-3.87-1.36-3.87-1.36-.53-1.34-1.3-1.7-1.3-1.7-1.06-.72.08-.71.08-.71 1.17.08 1.79 1.2 1.79 1.2 1.04 1.78 2.73 1.27 3.4.97.1-.75.4-1.27.74-1.56-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.04 0 0 .97-.31 3.18 1.19a11 11 0 0 1 5.8 0c2.2-1.5 3.17-1.19 3.17-1.19.63 1.58.23 2.75.12 3.04.73.81 1.18 1.84 1.18 3.1 0 4.43-2.7 5.4-5.27 5.69.41.36.78 1.05.78 2.12v3.14c0 .3.21.66.79.55A11.5 11.5 0 0 0 23.5 12 11.5 11.5 0 0 0 12 .5Z"
                />
              </svg>
              Star on GitHub
            </a>
          </div>
          <p className="support__credit">
            By{' '}
            <a href="https://github.com/mekedron" target="_blank" rel="noopener noreferrer">
              @mekedron
            </a>{' '}
            · 100% of tips go to keeping the project alive.
          </p>
        </div>

        <aside className="support__card" aria-label="What support unlocks">
          <header>
            <span className="support__card-eyebrow">What your tip funds</span>
          </header>
          <ul className="support__list">
            <li>
              <span className="support__dot" aria-hidden="true" />
              <div>
                <strong>New commands</strong>
                <span>discovery filters, batch carts, exports</span>
              </div>
            </li>
            <li>
              <span className="support__dot" aria-hidden="true" />
              <div>
                <strong>API drift fixes</strong>
                <span>quick patches when endpoints change</span>
              </div>
            </li>
            <li>
              <span className="support__dot" aria-hidden="true" />
              <div>
                <strong>Docs &amp; examples</strong>
                <span>real recipes, jq one-liners, scripts</span>
              </div>
            </li>
            <li>
              <span className="support__dot" aria-hidden="true" />
              <div>
                <strong>Coffee, honestly</strong>
                <span>literal coffee. it helps. a lot.</span>
              </div>
            </li>
          </ul>
        </aside>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  // useBaseUrl is invoked at render time so the build resolves /wolt-cli/ prefix correctly.
  useBaseUrl('/');
  return (
    <Layout
      title="wolt-cli — Unofficial Wolt CLI for the terminal"
      description="wolt-cli is an unofficial community Go CLI for browsing venues, menus, and carts from your terminal. Install with Homebrew, configure once, and shop without leaving the shell.">
      <div className="wcli-home">
        <Hero />
        <Trust />
        <Features />
        <Install />
        <Example />
        <Commands />
        <FAQ />
        <CTA />
        <Support />
      </div>
    </Layout>
  );
}

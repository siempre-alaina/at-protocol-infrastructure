const { PDS, envToCfg, envToSecrets, readEnv } = require('@atproto/pds');

async function main() {
  const env = readEnv();
  const cfg = envToCfg(env);
  const secrets = envToSecrets(env);

  const pds = await PDS.create(cfg, secrets);
  await pds.start();

  console.log('PDS is running');
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

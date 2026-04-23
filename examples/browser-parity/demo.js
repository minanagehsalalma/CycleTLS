let initCycleTLS;

try {
  initCycleTLS = require('../../dist/index.js');
} catch (localBuildError) {
  try {
    initCycleTLS = require('cycletls');
  } catch (packageError) {
    throw new Error(
      'Unable to load CycleTLS. Run "npm run build" from the repo root or install the published package before running this demo.'
    );
  }
}

const targetUrl = process.env.TARGET_URL;
const executablePath = process.env.CYCLETLS_EXECUTABLE_PATH;

const userAgent =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36';

const headers = {
  'sec-ch-ua': '"Not)A;Brand";v="8", "Chromium";v="138", "Google Chrome";v="138"',
  'sec-ch-ua-mobile': '?0',
  'sec-ch-ua-platform': '"Windows"',
  'upgrade-insecure-requests': '1',
  accept:
    'text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7',
  'sec-fetch-site': 'none',
  'sec-fetch-mode': 'navigate',
  'sec-fetch-user': '?1',
  'sec-fetch-dest': 'document',
  'accept-encoding': 'gzip, deflate, br, zstd',
  'accept-language': 'en-US,en;q=0.9',
  priority: 'u=0, i',
};

const headerOrder = [
  'sec-ch-ua',
  'sec-ch-ua-mobile',
  'sec-ch-ua-platform',
  'upgrade-insecure-requests',
  'user-agent',
  'accept',
  'sec-fetch-site',
  'sec-fetch-mode',
  'sec-fetch-user',
  'sec-fetch-dest',
  'accept-encoding',
  'accept-language',
  'priority',
];

async function main() {
  const cycleTLS = await initCycleTLS({
    timeout: 30_000,
    ...(executablePath ? { executablePath } : {}),
  });

  try {
    const probe = await cycleTLS.get('https://tls.peet.ws/api/all', {
      userAgent,
      headers,
      headerOrder,
      orderAsProvided: true,
      http2Fingerprint: '1:65536;2:0;4:6291456;6:262144|15663105|0|m,a,s,p',
      disableGrease: false,
    });

    const probeJson = await probe.json();
    const output = {
      probe: {
        ip: probeJson.ip,
        http_version: probeJson.http_version,
        ja3: probeJson.tls?.ja3,
        ja4_r: probeJson.tls?.ja4_r,
        akamai_fingerprint: probeJson.http2?.akamai_fingerprint,
      },
    };

    if (targetUrl) {
      const response = await cycleTLS.get(targetUrl, {
        userAgent,
        headers,
        headerOrder,
        orderAsProvided: true,
        http2Fingerprint: '1:65536;2:0;4:6291456;6:262144|15663105|0|m,a,s,p',
        disableGrease: false,
        timeout: 30_000,
      });

      output.target = {
        status: response.status,
        headers: response.headers,
      };
    }

    console.log(JSON.stringify(output, null, 2));
  } finally {
    await cycleTLS.exit();
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

import SwaggerParser from '@apidevtools/swagger-parser';
import { fileURLToPath } from 'node:url';
const specification = await SwaggerParser.validate(fileURLToPath(new URL('../../docs/openapi.yaml', import.meta.url)));
console.log(`OpenAPI ${specification.openapi} is valid; ${Object.keys(specification.paths).length} paths.`);

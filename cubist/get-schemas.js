const fs = require("fs");
const api = require("./openapi.json");

const RELEVANT_PATHS = [
  ["/v0/org/{org_id}/keys/{key_id}", ["get"]],
  ["/v1/org/{org_id}/blob/sign/{key_id}"],
  ["/v1/org/{org_id}/token/refresh"],
];

const getComponentKey = (ref) => {
  const path = ref.split("/");

  const componentName = path.pop();
  const componentType = path.pop();

  return [componentType, componentName];
};

const getComponent = (components, [componentType, componentName]) => {
  return components[componentType][componentName];
};

const assignComponent = (components, newComponents, key) => {
  newComponents[key[0]][key[1]] = getComponent(components, key);
};

const getRefs = (obj, refs = []) => {
  if (typeof obj !== "object" || !obj) return;

  if (obj.$ref) {
    refs.push(obj.$ref);
  }

  Object.values(obj).forEach((maybeObj) => {
    getRefs(maybeObj, refs);
  });

  return;
};

const components = api.components;
const newComponents = { ...api.components, schemas: {}, responses: {} };

let refs = [];

// get all relevant refs from paths
RELEVANT_PATHS.forEach(([path, methods = []]) => {
  if (methods.length === 0) {
    getRefs(api.paths[path], refs);
  } else {
    methods.forEach((method) => {
      getRefs(api.paths[path][method], refs);
    });
  }
});

// repeat
while (refs.length !== 0) {
  // get all relevant components from refs
  // and assign them to newComponents
  refs.forEach((ref) => {
    assignComponent(components, newComponents, getComponentKey(ref));
  });

  // reset refs to []
  refs = [];

  // get all relevant refs from newComponents.(schemas|responses)
  getRefs(newComponents.schemas, refs);
  getRefs(newComponents.responses, refs);

  // filter refs such that all refs that are already in newComponents are removed
  refs = refs.filter((ref) => {
    const [componentType, componentName] = getComponentKey(ref);
    return !newComponents[componentType][componentName];
  });
}

api.components = newComponents;
api.paths = RELEVANT_PATHS.reduce((acc, [path, methods = []]) => {
  if (methods.length === 0) {
    acc[path] = api.paths[path];
  } else {
    acc[path] = methods.reduce((acc, method) => {
      acc[method] = api.paths[path][method];
      return acc;
    }, {});
  }

  return acc;
}, {});

fs.writeFileSync("./filtered-openapi.json", JSON.stringify(api, null, 2));

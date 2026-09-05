export namespace core {
	
	export class CollectionSummary {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new CollectionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class EnvironmentSummary {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvironmentSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class WorkspaceInfo {
	    root: string;
	    collections: CollectionSummary[];
	    environments: EnvironmentSummary[];
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.collections = this.convertValues(source["collections"], CollectionSummary);
	        this.environments = this.convertValues(source["environments"], EnvironmentSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace domain {
	
	export class Auth {
	    type: string;
	    token?: string;
	    username?: string;
	    password?: string;
	    key?: string;
	    value?: string;
	
	    static createFrom(source: any = {}) {
	        return new Auth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.token = source["token"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.key = source["key"];
	        this.value = source["value"];
	    }
	}
	export class FormField {
	    key: string;
	    value: string;
	    enabled: boolean;
	    type?: string;
	    filePath?: string;
	
	    static createFrom(source: any = {}) {
	        return new FormField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	        this.type = source["type"];
	        this.filePath = source["filePath"];
	    }
	}
	export class Body {
	    mode: string;
	    raw?: string;
	    formFields?: FormField[];
	    binaryFilePath?: string;
	
	    static createFrom(source: any = {}) {
	        return new Body(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.raw = source["raw"];
	        this.formFields = this.convertValues(source["formFields"], FormField);
	        this.binaryFilePath = source["binaryFilePath"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Header {
	    key: string;
	    value: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Header(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	    }
	}
	export class QueryParam {
	    key: string;
	    value: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new QueryParam(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	    }
	}
	export class Item {
	    type: string;
	    id: string;
	    name: string;
	    items?: Item[];
	    method?: string;
	    url?: string;
	    params?: QueryParam[];
	    headers?: Header[];
	    auth?: Auth;
	    body?: Body;
	    preRequestScript?: string;
	    testScript?: string;
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.items = this.convertValues(source["items"], Item);
	        this.method = source["method"];
	        this.url = source["url"];
	        this.params = this.convertValues(source["params"], QueryParam);
	        this.headers = this.convertValues(source["headers"], Header);
	        this.auth = this.convertValues(source["auth"], Auth);
	        this.body = this.convertValues(source["body"], Body);
	        this.preRequestScript = source["preRequestScript"];
	        this.testScript = source["testScript"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Collection {
	    formatVersion: string;
	    id: string;
	    name: string;
	    description: string;
	    items: Item[];
	
	    static createFrom(source: any = {}) {
	        return new Collection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.formatVersion = source["formatVersion"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.items = this.convertValues(source["items"], Item);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Variable {
	    key: string;
	    value: string;
	    enabled: boolean;
	    secret: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Variable(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	        this.secret = source["secret"];
	    }
	}
	export class Environment {
	    formatVersion: string;
	    id: string;
	    name: string;
	    variables: Variable[];
	
	    static createFrom(source: any = {}) {
	        return new Environment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.formatVersion = source["formatVersion"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.variables = this.convertValues(source["variables"], Variable);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	

}

export namespace headercatalog {
	
	export class Entry {
	    name: string;
	    values?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.values = source["values"];
	    }
	}

}

export namespace httpengine {
	
	export class Response {
	    statusCode: number;
	    status: string;
	    headers: Record<string, Array<string>>;
	    body: string;
	    durationNs: number;
	    sizeBytes: number;
	    truncated?: boolean;
	    bodyFile?: string;
	
	    static createFrom(source: any = {}) {
	        return new Response(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.statusCode = source["statusCode"];
	        this.status = source["status"];
	        this.headers = source["headers"];
	        this.body = source["body"];
	        this.durationNs = source["durationNs"];
	        this.sizeBytes = source["sizeBytes"];
	        this.truncated = source["truncated"];
	        this.bodyFile = source["bodyFile"];
	    }
	}

}


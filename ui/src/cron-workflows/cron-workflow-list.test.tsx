import {render, waitFor} from '@testing-library/react';
import {createMemoryHistory} from 'history';
import React from 'react';
import {MemoryRouter} from 'react-router-dom';

import {Context, ContextApis} from '../shared/context';
import {CronWorkflow} from '../shared/models';
import {services} from '../shared/services';
import {CronWorkflowList} from './cron-workflow-list';

jest.mock('../shared/components/suspense-monaco-editor', () => ({
    SuspenseMonacoEditor: () => <textarea data-testid='editor' />
}));
jest.mock('../shared/services/requests');
jest.mock('../shared/services/cron-workflow-service', () => ({
    CronWorkflowService: {list: jest.fn()}
}));

function cronWorkflow(name: string, namespace: string): CronWorkflow {
    return {
        metadata: {name, namespace, creationTimestamp: '2024-01-01T00:00:00Z'},
        spec: {schedules: ['0 * * * *'], workflowSpec: {}},
        status: {active: [], conditions: [], lastScheduledTime: null}
    };
}

describe('CronWorkflowList', () => {
    beforeEach(() => {
        jest.clearAllMocks();
        jest.spyOn(services.info, 'collectEvent').mockResolvedValue(undefined as any);
    });

    afterEach(() => jest.restoreAllMocks());

    it('gives same-named cron workflows in different namespaces unique row keys', async () => {
        jest.spyOn(services.cronWorkflows, 'list').mockResolvedValue([cronWorkflow('build', 'ns-a'), cronWorkflow('build', 'ns-b')]);
        const history = createMemoryHistory();
        const context: ContextApis = {navigation: {} as any} as any;
        const consoleError = jest.spyOn(console, 'error').mockImplementation(() => undefined);

        render(
            <Context.Provider value={context}>
                <MemoryRouter>
                    <CronWorkflowList match={{params: {namespace: 'ns-a'}} as any} location={{search: ''} as any} history={history} />
                </MemoryRouter>
            </Context.Provider>
        );

        await waitFor(() => expect(document.querySelectorAll('.cron-workflows-list__row-container')).toHaveLength(2));
        expect(consoleError.mock.calls.filter(args => args.some(arg => typeof arg === 'string' && arg.includes('Encountered two children with the same key')))).toEqual([]);
    });
});

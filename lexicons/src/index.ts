/**
 * DashOfExtra Marketplace Lexicons
 *
 * TypeScript types for the AI Agent Marketplace Job Card lexicon.
 * These types correspond to the AT Protocol lexicon definitions in
 * com/dashofextra/marketplace/
 */

// Lexicon IDs
export const LEXICON_IDS = {
  JobCard: 'com.dashofextra.marketplace.jobCard',
  Defs: 'com.dashofextra.marketplace.defs',
} as const;

/**
 * Pricing information for a job
 */
export interface Pricing {
  /** Payment amount */
  amount?: number;
  /** Currency code (e.g., 'USD', 'EUR', 'BTC') */
  currency?: string;
}

/**
 * Service Level Agreement terms
 */
export interface SLA {
  /** Maximum time to respond to the job (e.g., '1h', '24h', '7d') */
  maxResponseTime?: string;
  /** Required availability (e.g., '99.9%', '24/7') */
  availability?: string;
}

/**
 * A Job Card representing a Call for Proposal in the AI agent marketplace.
 * Agents post these to solicit task proposals from other agents.
 */
export interface JobCard {
  /** The lexicon type identifier */
  $type: 'com.dashofextra.marketplace.jobCard';
  /** Detailed description of the task or job being requested */
  description: string;
  /** Category or type of task (e.g., 'data-analysis', 'code-review', 'content-generation') */
  taskType: string;
  /** Payment terms for the job */
  pricing?: Pricing;
  /** When the job must be completed by (ISO 8601 datetime) */
  deadline?: string;
  /** Service level agreement terms */
  sla?: SLA;
  /** List of representations and warranties required from the agent */
  repsAndWarranties?: string[];
  /** When this job card was created (ISO 8601 datetime) */
  createdAt: string;
}

/**
 * Input type for creating a Job Card (without $type)
 */
export type JobCardInput = Omit<JobCard, '$type'>;

/**
 * Helper function to create a Job Card record
 */
export function createJobCard(input: JobCardInput): JobCard {
  return {
    $type: 'com.dashofextra.marketplace.jobCard',
    ...input,
  };
}

/**
 * Type guard to check if a record is a Job Card
 */
export function isJobCard(record: unknown): record is JobCard {
  return (
    typeof record === 'object' &&
    record !== null &&
    '$type' in record &&
    (record as { $type: string }).$type === 'com.dashofextra.marketplace.jobCard'
  );
}

"""
Summarization service that uses Llama models to generate conversation summaries.
Processes transcribed text to extract insights, key points, and sentiment.
"""

import logging
import asyncio
from typing import Dict, Any, List, Optional
import time
import re

from ..models import model_manager, SummaryResult, TranscriptionResult
from ..models.llama_model import create_llama_model
from ..config import get_settings

logger = logging.getLogger(__name__)


class SummarizationService:
    """Service for AI-powered conversation summarization and analysis."""
    
    def __init__(self):
        self.settings = get_settings()
        self.llama_model = None
        self.is_initialized = False
    
    async def initialize(self, use_mock: bool = True) -> bool:
        """Initialize the summarization service with Llama model."""
        try:
            logger.info("Initializing summarization service...")
            
            # Create and register Llama model
            self.llama_model = create_llama_model(
                model_path=self.settings.llama_model_path,
                use_mock=use_mock  # Use mock for now until real model is available
            )
            
            model_manager.register_model("llama", self.llama_model)
            
            # Load the model
            success = await model_manager.load_model("llama")
            
            if success:
                self.is_initialized = True
                logger.info("Summarization service initialized successfully")
            else:
                logger.error("Failed to load Llama model")
            
            return success
            
        except Exception as e:
            logger.error(f"Error initializing summarization service: {e}")
            return False
    
    async def shutdown(self) -> bool:
        """Shutdown the summarization service."""
        try:
            if self.llama_model:
                await model_manager.unload_model("llama")
            
            self.is_initialized = False
            logger.info("Summarization service shutdown complete")
            return True
            
        except Exception as e:
            logger.error(f"Error shutting down summarization service: {e}")
            return False
    
    async def is_ready(self) -> bool:
        """Check if the service is ready for summarization."""
        return (
            self.is_initialized and 
            self.llama_model and 
            await self.llama_model.is_ready()
        )
    
    async def summarize_transcription(
        self, 
        transcription: TranscriptionResult,
        **kwargs
    ) -> SummaryResult:
        """
        Generate summary from transcription result.
        
        Args:
            transcription: TranscriptionResult to summarize
            **kwargs: Additional summarization options
            
        Returns:
            SummaryResult with summary, key points, and sentiment
        """
        if not await self.is_ready():
            raise RuntimeError("Summarization service not ready")
        
        try:
            # Convert transcription to text
            conversation_text = self._format_transcription_for_summary(transcription)
            
            logger.info(f"Summarizing conversation: {len(conversation_text)} characters")
            
            # Generate summary using Llama model
            summary_result = await self.llama_model.summarize(
                conversation_text,
                **kwargs
            )
            
            # Enhance summary with conversation-specific analysis
            enhanced_result = await self._enhance_summary(
                summary_result,
                transcription,
                conversation_text
            )
            
            logger.info(f"Summary generated: {len(enhanced_result.text)} characters")
            
            return enhanced_result
            
        except Exception as e:
            logger.error(f"Summarization failed: {e}")
            raise
    
    def _format_transcription_for_summary(self, transcription: TranscriptionResult) -> str:
        """Format transcription segments into readable conversation text."""
        if not transcription.segments:
            return ""
        
        # Group segments by speaker and format as dialogue
        formatted_lines = []
        
        for segment in transcription.segments:
            # Clean up the text
            text = segment.text.strip()
            if not text:
                continue
            
            # Format with speaker label and timestamp
            speaker_label = "Agent" if segment.speaker == "left" else "Customer"
            timestamp = f"[{segment.start_time:.1f}s]"
            
            formatted_lines.append(f"{speaker_label} {timestamp}: {text}")
        
        return "\n".join(formatted_lines)
    
    async def _enhance_summary(
        self,
        summary_result: SummaryResult,
        transcription: TranscriptionResult,
        conversation_text: str
    ) -> SummaryResult:
        """Enhance summary with additional conversation analysis."""
        try:
            # Add conversation statistics
            stats = self._calculate_conversation_stats(transcription)
            
            # Detect conversation type
            conversation_type = await self._detect_conversation_type(conversation_text)
            
            # Enhance key points with speaker attribution
            enhanced_key_points = await self._enhance_key_points(
                summary_result.key_points,
                transcription
            )
            
            # Create enhanced summary text
            enhanced_text = self._create_enhanced_summary_text(
                summary_result.text,
                stats,
                conversation_type
            )
            
            return SummaryResult(
                text=enhanced_text,
                key_points=enhanced_key_points,
                sentiment=summary_result.sentiment,
                confidence=summary_result.confidence,
                processing_time=summary_result.processing_time,
                model_version=summary_result.model_version
            )
            
        except Exception as e:
            logger.warning(f"Summary enhancement failed: {e}")
            return summary_result  # Return original if enhancement fails
    
    def _calculate_conversation_stats(self, transcription: TranscriptionResult) -> Dict[str, Any]:
        """Calculate conversation statistics."""
        if not transcription.segments:
            return {}
        
        left_segments = [s for s in transcription.segments if s.speaker == "left"]
        right_segments = [s for s in transcription.segments if s.speaker == "right"]
        
        left_words = sum(len(s.text.split()) for s in left_segments)
        right_words = sum(len(s.text.split()) for s in right_segments)
        
        left_talk_time = sum(s.end_time - s.start_time for s in left_segments)
        right_talk_time = sum(s.end_time - s.start_time for s in right_segments)
        
        return {
            "total_segments": len(transcription.segments),
            "left_segments": len(left_segments),
            "right_segments": len(right_segments),
            "left_words": left_words,
            "right_words": right_words,
            "left_talk_time": left_talk_time,
            "right_talk_time": right_talk_time,
            "total_words": left_words + right_words,
            "duration": transcription.duration,
            "avg_confidence": transcription.confidence
        }
    
    async def _detect_conversation_type(self, conversation_text: str) -> str:
        """Detect the type of conversation (customer service, meeting, etc.)."""
        text_lower = conversation_text.lower()
        
        # Simple keyword-based detection
        if any(word in text_lower for word in ["account", "billing", "support", "help", "issue", "problem"]):
            return "customer_service"
        elif any(word in text_lower for word in ["meeting", "agenda", "action", "decision", "project"]):
            return "meeting"
        elif any(word in text_lower for word in ["sale", "purchase", "product", "price", "buy"]):
            return "sales"
        elif any(word in text_lower for word in ["interview", "position", "experience", "qualification"]):
            return "interview"
        else:
            return "general"
    
    async def _enhance_key_points(
        self,
        key_points: List[str],
        transcription: TranscriptionResult
    ) -> List[str]:
        """Enhance key points with speaker context where relevant."""
        if not key_points or not transcription.segments:
            return key_points
        
        enhanced_points = []
        
        for point in key_points:
            # Try to find which speaker mentioned this point
            point_lower = point.lower()
            
            # Find segments that might relate to this point
            related_segments = []
            for segment in transcription.segments:
                segment_words = set(segment.text.lower().split())
                point_words = set(point_lower.split())
                
                # Check for word overlap
                if len(segment_words.intersection(point_words)) >= 2:
                    related_segments.append(segment)
            
            if related_segments:
                # Determine primary speaker for this point
                left_count = sum(1 for s in related_segments if s.speaker == "left")
                right_count = sum(1 for s in related_segments if s.speaker == "right")
                
                if left_count > right_count:
                    enhanced_points.append(f"{point} (Agent)")
                elif right_count > left_count:
                    enhanced_points.append(f"{point} (Customer)")
                else:
                    enhanced_points.append(point)
            else:
                enhanced_points.append(point)
        
        return enhanced_points
    
    def _create_enhanced_summary_text(
        self,
        original_summary: str,
        stats: Dict[str, Any],
        conversation_type: str
    ) -> str:
        """Create enhanced summary with metadata."""
        if not original_summary:
            return "No summary available."
        
        enhanced_parts = [original_summary]
        
        # Add conversation type if detected
        if conversation_type != "general":
            type_labels = {
                "customer_service": "Customer Service",
                "meeting": "Meeting",
                "sales": "Sales",
                "interview": "Interview"
            }
            enhanced_parts.append(f"\nConversation Type: {type_labels.get(conversation_type, conversation_type)}")
        
        # Add key statistics if available
        if stats:
            duration_min = stats.get("duration", 0) / 60
            total_words = stats.get("total_words", 0)
            
            if duration_min > 0 and total_words > 0:
                enhanced_parts.append(f"\nDuration: {duration_min:.1f} minutes")
                enhanced_parts.append(f"Total words: {total_words}")
                
                # Speaking time distribution
                left_time = stats.get("left_talk_time", 0)
                right_time = stats.get("right_talk_time", 0)
                total_talk = left_time + right_time
                
                if total_talk > 0:
                    left_pct = (left_time / total_talk) * 100
                    right_pct = (right_time / total_talk) * 100
                    enhanced_parts.append(f"Speaking time: Agent {left_pct:.0f}%, Customer {right_pct:.0f}%")
        
        return "\n".join(enhanced_parts)
    
    async def analyze_sentiment_details(self, conversation_text: str) -> Dict[str, Any]:
        """Perform detailed sentiment analysis."""
        try:
            # Use Llama for detailed sentiment analysis
            prompt = f"""Analyze the sentiment and emotional tone of this conversation in detail:

{conversation_text}

Provide analysis in JSON format with:
- overall_sentiment: positive/negative/neutral
- confidence: 0.0-1.0
- emotional_indicators: list of detected emotions
- speaker_sentiments: sentiment for each speaker
- key_emotional_moments: significant emotional points

Analysis:"""
            
            response = await self.llama_model.generate(
                prompt,
                max_tokens=256,
                temperature=0.1
            )
            
            # Try to parse JSON response
            try:
                import json
                sentiment_data = json.loads(response)
                return sentiment_data
            except:
                # Fallback to simple parsing
                return {
                    "overall_sentiment": self._extract_sentiment_from_text(response),
                    "confidence": 0.7,
                    "analysis_text": response
                }
                
        except Exception as e:
            logger.error(f"Detailed sentiment analysis failed: {e}")
            return {"error": str(e)}
    
    def _extract_sentiment_from_text(self, text: str) -> str:
        """Extract sentiment from text response."""
        text_lower = text.lower()
        
        if "positive" in text_lower:
            return "positive"
        elif "negative" in text_lower:
            return "negative"
        else:
            return "neutral"
    
    async def get_service_status(self) -> Dict[str, Any]:
        """Get current service status and statistics."""
        model_info = model_manager.get_model_info("llama")
        health = await model_manager.health_check()
        
        return {
            "initialized": self.is_initialized,
            "ready": await self.is_ready(),
            "model_info": model_info.dict() if model_info else None,
            "health": health,
            "settings": {
                "max_tokens": self.settings.llama_max_tokens,
                "temperature": self.settings.llama_temperature,
                "top_p": self.settings.llama_top_p
            }
        }


# Global summarization service instance
summarization_service = SummarizationService()
